package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kelseyhightower/envconfig"
	ollama "github.com/ollama/ollama/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/qri-io/jsonschema"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	APIKey         string `envconfig:"API_KEY"`
	URL            string `default:"http://localhost:11434/v1/"`
	Model          string `default:"qwen2.5:7b"`
	EmbeddingModel string `split_words:"true" default:"nomic-embed-text"`
}

func DefaultConfig() Config {
	c := Config{}
	if err := envconfig.Process("", &c); err != nil {
		slog.Error("error processing environment variables", "error", err)
		os.Exit(1)
	}
	return c
}

type Client interface {
	Config() Config
	GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error)
	CreateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error)
	GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error)
	RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, string, error)
	ClassifyCommand(ctx context.Context, text string) (string, error)
}

type client struct {
	client *openai.Client
	cfg    Config
	db     *pgxpool.Pool
}

func New(ctx context.Context, cfg Config, db *pgxpool.Pool) (Client, error) {
	if cfg.URL != "http://localhost:11434/v1/" && cfg.APIKey == "" {
		return nil, nil
	}

	openaiClient := openai.NewClient(option.WithBaseURL(cfg.URL), option.WithAPIKey(cfg.APIKey))
	if _, err := openaiClient.Models.Get(ctx, cfg.Model); err != nil {
		var aerr *openai.Error
		if errors.As(err, &aerr) && aerr.StatusCode == http.StatusNotFound && cfg.URL == "http://localhost:11434/v1/" {
			if err := downloadOllamaModel(ctx, cfg.Model); err != nil {
				return nil, fmt.Errorf("downloading model %s: %w", cfg.Model, err)
			}
		} else {
			return nil, fmt.Errorf("getting model: %w", err)
		}
	}

	if _, err := openaiClient.Models.Get(ctx, cfg.EmbeddingModel); err != nil {
		var aerr *openai.Error
		if errors.As(err, &aerr) && aerr.StatusCode == http.StatusNotFound && cfg.URL == "http://localhost:11434/v1/" {
			if err := downloadOllamaModel(ctx, cfg.EmbeddingModel); err != nil {
				return nil, fmt.Errorf("downloading embedding model %s: %w", cfg.EmbeddingModel, err)
			}
		} else {
			return nil, fmt.Errorf("getting embedding model: %w", err)
		}
	}

	return &client{
		client: openaiClient,
		cfg:    cfg,
		db:     db,
	}, nil
}

func downloadOllamaModel(ctx context.Context, s string) error {
	client := ollama.NewClient(&url.URL{
		Scheme: "http",
		Host:   "localhost:11434",
	}, http.DefaultClient)
	if err := client.Pull(ctx, &ollama.PullRequest{
		Name: s,
	}, func(resp ollama.ProgressResponse) error {
		fmt.Fprintf(os.Stderr, "\r%s: %s [%d/%d]", s, resp.Status, resp.Completed, resp.Total)
		return nil
	}); err != nil {
		return fmt.Errorf("downloading model %s: %w", s, err)
	}

	slog.DebugContext(ctx, "downloaded model", "model", s)
	return nil
}

// persistLLMUsage directly writes LLM usage data to the database
// This is a best-effort operation and failures are logged but don't affect the main flow
func (c *client) persistLLMUsage(ctx context.Context, input dto.LLMInput, output dto.LLMOutput, model string) {
	if c.db == nil {
		slog.DebugContext(ctx, "database connection not initialized, skipping LLM usage persistence")
		return
	}

	params := schema.AddLLMUsageParams{
		Input:  input,
		Output: output,
		Model:  model,
	}

	if _, err := schema.New(c.db).AddLLMUsage(ctx, params); err != nil {
		slog.WarnContext(ctx, "failed to persist LLM usage", "error", err)
	}
}

func (c *client) Config() Config {
	return c.cfg
}

func (c *client) GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a technical analyst reviewing user support messages. Your task is to identify specific, actionable improvements based on the provided messages.

	For each suggestion:
	1. Focus only on concrete issues mentioned in the messages
	2. Provide a clear, specific title that identifies the problem area
	3. Include a single, concise bullet point explaining the proposed solution
	4. Format in Slack-friendly markdown with each suggestion as a separate block

	Rules:
	- Maximum 3 suggestions
	- Each suggestion must directly relate to issues in the messages
	- No generic or speculative improvements
	- Keep titles short and descriptive
	- Bullet points should be 1-2 sentences maximum
	- If no clear improvements can be identified, return "No specific improvements identified from these messages"

	Format each suggestion as:
	*Title*
	• Specific improvement details
	`

	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.cfg.Model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleSystem),
				Content: openai.F(any(prompt)),
			},
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleUser),
				Content: openai.F(any(fmt.Sprintf("Messages:\n%s", messages))),
			},
		}),
		Temperature: openai.F(0.7),
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("generating suggestions: %w", err)
	}

	slog.DebugContext(ctx, "generated suggestions", "request", params, "response", resp.Choices[0].Message.Content)

	// Persist LLM usage (best effort)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Messages: []dto.LLMMessage{
				{Role: "system", Content: prompt},
				{Role: "user", Content: fmt.Sprintf("Messages:\n%s", messages)},
			},
		},
		dto.LLMOutput{
			Content: resp.Choices[0].Message.Content,
			Usage: &dto.LLMUsageStats{
				PromptTokens:     int(resp.Usage.PromptTokens),
				CompletionTokens: int(resp.Usage.CompletionTokens),
				TotalTokens:      int(resp.Usage.TotalTokens),
			},
		},
		string(c.cfg.Model),
	)

	return resp.Choices[0].Message.Content, nil
}

type RunbookResponse struct {
	AlertOverview        string   `json:"alert_overview"`
	HistoricalRootCauses []string `json:"historical_root_causes"`
	ResolutionSteps      []string `json:"resolution_steps"`
	LexicalSearchQuery   string   `json:"lexical_search_query"`
	SemanticSearchQuery  string   `json:"semantic_search_query"`
}

func (c *client) CreateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error) {
	if c == nil {
		return nil, nil
	}

	prompt := `Create a structured runbook based on the provided service, alert, and incident messages. Return a JSON object with these exact fields:

{
  "alert_overview": "Brief description of the alert and what triggers it (2-3 sentences)",
  "historical_root_causes": ["List of specific causes from past incidents"],
  "resolution_steps": ["Concrete troubleshooting steps that were successful, with commands and outcomes"],
  "lexical_search_query": "A keyword-based search query with exact terms, error codes, and component names for exact matching",
  "semantic_search_query": "A natural language query describing the problem conceptually for semantic/meaning-based search"
}

RULES:
- Include only information explicitly mentioned in the messages
- Keep content specific and actionable
- Focus on technical details and specific patterns
- Return empty arrays if no relevant information exists for a section

EXAMPLES:

Example 1:
{
  "alert_overview": "High latency alert for the payment processing service. Triggers when p99 latency exceeds 500ms for 5 consecutive minutes.",
  "historical_root_causes": [
    "Database connection pool exhaustion due to connection leaks",
    "Redis cache misses causing increased DB load"
  ],
  "resolution_steps": [
    "Check connection pool metrics: kubectl get metrics -n payments | grep pool_size",
    "Restart affected pods if connection count > 80%: kubectl rollout restart deployment/payment-svc"
  ],
  "lexical_search_query": "payment-svc latency 500ms connection pool redis cache",
  "semantic_search_query": "payment service high latency issues related to database connections and caching"
}

Example 2:
{
  "alert_overview": "Memory usage exceeded threshold on authentication service. Alert fires when memory usage is above 85% for 10 minutes.",
  "historical_root_causes": [
    "Memory leak in JWT validation routine",
    "Large session objects not being garbage collected"
  ],
  "resolution_steps": [
    "Review memory profile: curl localhost:6060/debug/pprof/heap > heap.out",
    "Temporary mitigation: kubectl rollout restart deployment/auth-svc"
  ],
  "lexical_search_query": "auth-svc OOM JWT validation memory leak pprof heap",
  "semantic_search_query": "authentication service memory issues related to JWT validation and session object garbage collection"
}

Example 3:
{
  "alert_overview": "Disk usage alert for logging service. Triggers when disk utilization exceeds 90% on logging nodes.",
  "historical_root_causes": [
    "Log rotation not cleaning up old files properly",
    "Sudden spike in debug logging from application"
  ],
  "resolution_steps": [
    "Check disk usage: df -h /var/log",
    "Force log rotation: logrotate -f /etc/logrotate.d/application",
    "Compress old logs: find /var/log -name '*.log.*' -exec gzip {} \\;"
  ],
  "lexical_search_query": "logging disk usage 90% logrotate /var/log",
  "semantic_search_query": "logging service disk space issues related to log rotation and cleanup"
}

Example 4:
{
  "alert_overview": "API error rate alert for user service. Triggers when 5xx errors exceed 5% over 5 minutes.",
  "historical_root_causes": [
    "Rate limiting configuration too aggressive",
    "Database deadlocks during peak traffic"
  ],
  "resolution_steps": [
    "Check error distribution: kubectl logs -l app=user-svc | grep ERROR | sort | uniq -c",
    "Analyze rate limit metrics: curl -s localhost:9090/metrics | grep rate_limit",
    "Scale up replicas if needed: kubectl scale deployment/user-svc --replicas=5"
  ],
  "lexical_search_query": "user-svc 5xx error rate_limit database deadlock",
  "semantic_search_query": "user service API errors related to rate limiting and database deadlocks during high traffic"
}`

	content := fmt.Sprintf("Service: %s\nAlert: %s\nMessages:\n", service, alert)
	for _, msg := range msgs {
		content += msg + "\n"
	}

	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.cfg.Model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleSystem),
				Content: openai.F(any(prompt)),
			},
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleUser),
				Content: openai.F(any(content)),
			},
		}),
		Temperature: openai.F(0.7),
		ResponseFormat: openai.F[openai.ChatCompletionNewParamsResponseFormatUnion](
			openai.ResponseFormatJSONObjectParam{
				Type: openai.F[openai.ResponseFormatJSONObjectType](openai.ResponseFormatJSONObjectTypeJSONObject),
			},
		),
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("creating runbook: %w", err)
	}

	slog.DebugContext(ctx, "created runbook", "request", params, "response", resp.Choices[0].Message.Content)

	// Persist LLM usage (best effort)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Messages: []dto.LLMMessage{
				{Role: "system", Content: prompt},
				{Role: "user", Content: content},
			},
		},
		dto.LLMOutput{
			Content: resp.Choices[0].Message.Content,
			Usage: &dto.LLMUsageStats{
				PromptTokens:     int(resp.Usage.PromptTokens),
				CompletionTokens: int(resp.Usage.CompletionTokens),
				TotalTokens:      int(resp.Usage.TotalTokens),
			},
		},
		string(c.cfg.Model),
	)

	var runbook RunbookResponse
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &runbook); err != nil {
		return nil, fmt.Errorf("unmarshaling runbook response: %w", err)
	}

	return &runbook, nil
}

func (c *client) GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error) {
	if c == nil {
		return nil, nil
	}

	params := openai.EmbeddingNewParams{
		Model: openai.F(openai.EmbeddingModel(c.cfg.EmbeddingModel)),
		Input: openai.F(openai.EmbeddingNewParamsInputUnion(
			openai.EmbeddingNewParamsInputArrayOfStrings([]string{fmt.Sprintf("%s: %s", task, text)}),
		)),
		EncodingFormat: openai.F(openai.EmbeddingNewParamsEncodingFormatFloat),
		Dimensions:     openai.F(int64(768)),
	}

	resp, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	// Persist LLM usage (best effort)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Text: fmt.Sprintf("%s: %s", task, text),
		},
		dto.LLMOutput{
			Embedding: make([]float32, len(resp.Data[0].Embedding)),
			Usage: &dto.LLMUsageStats{
				PromptTokens: int(resp.Usage.PromptTokens),
				TotalTokens:  int(resp.Usage.TotalTokens),
			},
		},
		string(c.cfg.EmbeddingModel),
	)

	r := make([]float32, len(resp.Data[0].Embedding))
	for i, v := range resp.Data[0].Embedding {
		r[i] = float32(v)
	}

	return r, nil
}

// RunJSONModePrompt runs a JSON mode prompt and validates the response against the provided JSON schema.
// It returns the response and the raw response message (returned if the response is not valid).
func (c *client) RunJSONModePrompt(ctx context.Context, prompt string, jsonSchema *jsonschema.Schema) (string, string, error) {
	if c == nil {
		return "", "", nil
	}
	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.cfg.Model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleSystem),
				Content: openai.F(any(prompt)),
			},
		}),
		Temperature: openai.F(0.7),
		ResponseFormat: openai.F[openai.ChatCompletionNewParamsResponseFormatUnion](
			openai.ResponseFormatJSONObjectParam{
				Type: openai.F[openai.ResponseFormatJSONObjectType](openai.ResponseFormatJSONObjectTypeJSONObject),
			},
		)}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", "", fmt.Errorf("running JSON mode prompt: %w", err)
	}
	respMsg := resp.Choices[0].Message.Content
	slog.DebugContext(ctx, "ran JSON mode prompt", "request", params, "response", respMsg)

	// Persist LLM usage (best effort)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Messages: []dto.LLMMessage{
				{Role: "system", Content: prompt},
			},
		},
		dto.LLMOutput{
			Content: respMsg,
			Usage: &dto.LLMUsageStats{
				PromptTokens:     int(resp.Usage.PromptTokens),
				CompletionTokens: int(resp.Usage.CompletionTokens),
				TotalTokens:      int(resp.Usage.TotalTokens),
			},
		},
		string(c.cfg.Model),
	)

	if jsonSchema != nil {
		if keyErr, err := jsonSchema.ValidateBytes(ctx, []byte(respMsg)); err != nil || len(keyErr) > 0 {
			return "", respMsg, fmt.Errorf("validating response: %v %w", keyErr, err)
		}
	}
	return respMsg, "", nil
}

func (c *client) ClassifyCommand(ctx context.Context, text string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a command classifier that identifies the most appropriate command from user input. 
Given a message, respond with exactly one of these commands:
- weekly_report
- usage_report
- none

Examples:
User: "generate weekly incident report for this channel"
Response: weekly_report

User: "post report"
Response: weekly_report

User: "what's the status report"
Response: weekly_report

User: "show ratchet bot usage statistics"
Response: usage_report

User: "post usage report"
Response: usage_report

User: "how are you doing?"
Response: none

User: "what's the weather like?"
Response: none

Classify the following message:`

	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.cfg.Model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleSystem),
				Content: openai.F(any(prompt)),
			},
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleUser),
				Content: openai.F(any(text)),
			},
		}),
		Temperature: openai.F(0.0), // Use 0 temperature for more consistent results
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("classifying command: %w", err)
	}

	// Persist LLM usage (best effort)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Messages: []dto.LLMMessage{
				{Role: "system", Content: prompt},
				{Role: "user", Content: text},
			},
		},
		dto.LLMOutput{
			Content: resp.Choices[0].Message.Content,
			Usage: &dto.LLMUsageStats{
				PromptTokens:     int(resp.Usage.PromptTokens),
				CompletionTokens: int(resp.Usage.CompletionTokens),
				TotalTokens:      int(resp.Usage.TotalTokens),
			},
		},
		string(c.cfg.Model),
	)

	return resp.Choices[0].Message.Content, nil
}
