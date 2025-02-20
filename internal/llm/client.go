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

	"github.com/kelseyhightower/envconfig"
	ollama "github.com/ollama/ollama/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/qri-io/jsonschema"

	"github.com/dynoinc/ratchet/internal/storage/schema"
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
	CreateRunbook(ctx context.Context, service string, alert string, msgs []schema.ThreadMessagesV2) (*RunbookResponse, error)
	GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error)
	RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, error)
}

type client struct {
	client *openai.Client
	cfg    Config
}

func New(ctx context.Context, cfg Config) (Client, error) {
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

	return resp.Choices[0].Message.Content, nil
}

type RunbookResponse struct {
	AlertOverview        string   `json:"alert_overview"`
	HistoricalRootCauses []string `json:"historical_root_causes"`
	ResolutionSteps      []string `json:"resolution_steps"`
	SearchQuery          string   `json:"search_query"`
}

func (c *client) CreateRunbook(ctx context.Context, service string, alert string, msgs []schema.ThreadMessagesV2) (*RunbookResponse, error) {
	if c == nil {
		return nil, nil
	}

	prompt := `Create a structured runbook based on the provided service, alert, and incident messages. Return a JSON object with these exact fields:

{
  "alert_overview": "Brief description of the alert and what triggers it (2-3 sentences)",
  "historical_root_causes": ["List of specific causes from past incidents"],
  "resolution_steps": ["Concrete troubleshooting steps that were successful, with commands and outcomes"],
  "search_query": "A search query containing key technical terms, error patterns, and components involved in this alert"
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
  "search_query": "payment service latency database connection pool redis cache performance"
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
  "search_query": "auth service memory leak JWT validation session objects garbage collection OOM"
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
  "search_query": "logging service disk usage log rotation cleanup compression storage"
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
  "search_query": "user service API 5xx errors rate limiting database deadlocks high traffic"
}`

	content := fmt.Sprintf("Service: %s\nAlert: %s\nMessages:\n", service, alert)
	for _, msg := range msgs {
		content += fmt.Sprintf("%s\n", msg.Attrs.Message.Text)
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

	r := make([]float32, len(resp.Data[0].Embedding))
	for i, v := range resp.Data[0].Embedding {
		r[i] = float32(v)
	}

	return r, nil
}

func (c *client) RunJSONModePrompt(ctx context.Context, prompt string, jsonSchema *jsonschema.Schema) (string, error) {
	if c == nil {
		return "", nil
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
		return "", fmt.Errorf("running JSON mode prompt: %w", err)
	}
	respMsg := resp.Choices[0].Message.Content
	slog.DebugContext(ctx, "ran JSON mode prompt", "request", params, "response", respMsg)
	if jsonSchema != nil {
		if keyErr, err := jsonSchema.ValidateBytes(ctx, []byte(respMsg)); err != nil || len(keyErr) > 0 {
			return "", fmt.Errorf("validating response: %v %w", keyErr, err)
		}
	}
	return respMsg, nil
}
