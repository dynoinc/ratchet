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
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"
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

	GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error)
	GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error)
	GenerateDocumentationResponse(ctx context.Context, question string, documents []string) (string, error)
	GenerateDocumentationUpdate(ctx context.Context, doc string, msgs string) (string, error)
	GenerateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error)
	RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, string, error)
	ClassifyCommand(ctx context.Context, text string, sampleMessages map[string][]string) (string, error)
}

type client struct {
	client openai.Client
	cfg    Config
	db     *pgxpool.Pool
}

func New(ctx context.Context, cfg Config, db *pgxpool.Pool) (Client, error) {
	if cfg.URL != "http://localhost:11434/v1/" && cfg.APIKey == "" {
		return nil, nil
	}

	openaiClient := openai.NewClient(option.WithBaseURL(cfg.URL), option.WithAPIKey(cfg.APIKey))

	// Check and download the main model if needed
	if err := checkAndDownloadModel(ctx, openaiClient, cfg.Model, cfg.URL); err != nil {
		return nil, err
	}

	// Check and download the embedding model if needed
	if err := checkAndDownloadModel(ctx, openaiClient, cfg.EmbeddingModel, cfg.URL); err != nil {
		return nil, err
	}

	return &client{
		client: openaiClient,
		cfg:    cfg,
		db:     db,
	}, nil
}

func checkAndDownloadModel(ctx context.Context, client openai.Client, modelName string, baseURL string) error {
	if _, err := client.Models.Get(ctx, modelName); err != nil {
		var aerr *openai.Error
		if errors.As(err, &aerr) && aerr.StatusCode == http.StatusNotFound && baseURL == "http://localhost:11434/v1/" {
			if err := downloadOllamaModel(ctx, modelName); err != nil {
				return fmt.Errorf("downloading model %s: %w", modelName, err)
			}
		} else {
			return fmt.Errorf("getting model %s: %w", modelName, err)
		}
	}
	return nil
}

func downloadOllamaModel(ctx context.Context, s string) error {
	client := ollama.NewClient(&url.URL{
		Scheme: "http",
		Host:   "localhost:11434",
	}, http.DefaultClient)
	if err := client.Pull(ctx, &ollama.PullRequest{
		Model: s,
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

// runChatCompletion is a helper function to run chat completions with common logic
func (c *client) runChatCompletion(
	ctx context.Context,
	inputMessages []dto.LLMMessage,
	messages []openai.ChatCompletionMessageParamUnion,
	temperature float64,
	jsonMode bool,
) (string, error) {
	if c == nil {
		return "", nil
	}

	params := openai.ChatCompletionNewParams{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: param.NewOpt(temperature),
	}

	if jsonMode {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", err
	}

	content := resp.Choices[0].Message.Content

	// Persist LLM usage
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Messages: inputMessages,
		},
		dto.LLMOutput{
			Content: content,
			Usage: &dto.LLMUsageStats{
				PromptTokens:     int(resp.Usage.PromptTokens),
				CompletionTokens: int(resp.Usage.CompletionTokens),
				TotalTokens:      int(resp.Usage.TotalTokens),
			},
		},
		c.cfg.Model,
	)

	return content, nil
}

// createLLMMessages creates a slice of message params from system and user content
func createLLMMessages(systemContent string, userContent string) ([]openai.ChatCompletionMessageParamUnion, []dto.LLMMessage) {
	messages := []openai.ChatCompletionMessageParamUnion{
		{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: param.NewOpt(systemContent),
				},
			},
		},
	}

	inputMessages := []dto.LLMMessage{
		{Role: "system", Content: systemContent},
	}

	if userContent != "" {
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: param.NewOpt(userContent),
				},
			},
		})
		inputMessages = append(inputMessages, dto.LLMMessage{Role: "user", Content: userContent})
	}

	return messages, inputMessages
}

func (c *client) GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a technical analyst reviewing Slack support channel messages. Your task is to identify specific, actionable improvements to reduce operational toil based on the provided conversation threads.

	Analyze the messages for:
	1. Recurring technical issues that could be automated
	2. Knowledge gaps that could be addressed with better documentation
	3. Process inefficiencies that slow down incident resolution
	4. Alert fatigue or noisy monitoring that could be optimized

	For each suggestion:
	1. Focus only on concrete issues with evidence in the messages
	2. Provide a clear, specific title that identifies the problem area
	3. Include a single, concise bullet point explaining the proposed solution and its expected impact

	Rules:
	- Maximum 3 high-impact suggestions
	- Each suggestion must directly relate to issues in the messages
	- No generic or speculative improvements
	- Keep titles short and descriptive (5-7 words)
	- Bullet points should be 1-2 sentences maximum
	- If no clear improvements can be identified, return "No specific improvements identified from these messages"

	Format each suggestion as:
	*Title*
	• Specific improvement details
	`

	userContent := fmt.Sprintf("Messages:\n%s", messages)
	chatMessages, inputMessages := createLLMMessages(prompt, userContent)

	content, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, false)
	if err != nil {
		return "", fmt.Errorf("generating suggestions: %w", err)
	}

	slog.DebugContext(ctx, "generated suggestions", "response", content)

	return content, nil
}

type RunbookResponse struct {
	AlertOverview        string   `json:"alert_overview"`
	HistoricalRootCauses []string `json:"historical_root_causes"`
	ResolutionSteps      []string `json:"resolution_steps"`
	LexicalSearchQuery   string   `json:"lexical_search_query"`
	SemanticSearchQuery  string   `json:"semantic_search_query"`
}

func (c *client) GenerateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error) {
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
- Keep content specific, technical, and actionable
- Focus on technical details, error patterns, and resolution steps
- Use bullet points for clarity in the JSON arrays
- Prioritize commands, error codes, and specific metrics when available
- Return empty arrays if no relevant information exists for a section
- Ensure lexical search query contains exact technical terms for precise matching
- Make semantic search query descriptive enough to capture conceptual similarities

EXAMPLES:

Example 1:
{
  "alert_overview": "High latency alert for the payment processing service. Triggers when p99 latency exceeds 500ms for 5 consecutive minutes.",
  "historical_root_causes": [
    "Database connection pool exhaustion due to connection leaks",
    "Redis cache misses causing increased DB load",
    "Network congestion between payment service and payment gateway"
  ],
  "resolution_steps": [
    "Check connection pool metrics: kubectl get metrics -n payments | grep pool_size",
    "Restart affected pods if connection count > 80%: kubectl rollout restart deployment/payment-svc",
    "Verify Redis cache hit ratio: redis-cli info stats | grep hit_rate"
  ],
  "lexical_search_query": "payment-svc latency 500ms connection pool redis cache timeout",
  "semantic_search_query": "payment service high latency issues related to database connections, redis caching, and network timeouts"
}

Example 2:
{
  "alert_overview": "Memory usage exceeded threshold on authentication service. Alert fires when memory usage is above 85% for 10 minutes.",
  "historical_root_causes": [
    "Memory leak in JWT validation routine",
    "Large session objects not being garbage collected",
    "Excessive concurrent requests during peak hours"
  ],
  "resolution_steps": [
    "Review memory profile: curl localhost:6060/debug/pprof/heap > heap.out",
    "Analyze heap dump: go tool pprof -http=:8080 heap.out",
    "Temporary mitigation: kubectl rollout restart deployment/auth-svc"
  ],
  "lexical_search_query": "auth-svc OOM JWT validation memory leak pprof heap garbage collection",
  "semantic_search_query": "authentication service memory issues related to JWT validation, session object garbage collection, and request handling during peak load"
}

Example 3:
{
  "alert_overview": "Disk usage alert for logging service. Triggers when disk utilization exceeds 90% on logging nodes.",
  "historical_root_causes": [
    "Log rotation not cleaning up old files properly",
    "Sudden spike in debug logging from application",
    "Insufficient disk space allocation for log volume"
  ],
  "resolution_steps": [
    "Check disk usage: df -h /var/log",
    "Force log rotation: logrotate -f /etc/logrotate.d/application",
    "Compress old logs: find /var/log -name '*.log.*' -exec gzip {} \\;",
    "Increase log volume if needed: lvcreate -L +10G -n log_vol vg_logs"
  ],
  "lexical_search_query": "logging disk usage 90% logrotate /var/log compression retention",
  "semantic_search_query": "logging service disk space issues related to log rotation, cleanup, and volume management"
}

Example 4:
{
  "alert_overview": "API error rate alert for user service. Triggers when 5xx errors exceed 5% over 5 minutes.",
  "historical_root_causes": [
    "Rate limiting configuration too aggressive",
    "Database deadlocks during peak traffic",
    "Timeout in downstream dependency causing cascading failures"
  ],
  "resolution_steps": [
    "Check error distribution: kubectl logs -l app=user-svc | grep ERROR | sort | uniq -c",
    "Analyze rate limit metrics: curl -s localhost:9090/metrics | grep rate_limit",
    "Check database locks: SELECT * FROM pg_locks WHERE granted = false;",
    "Scale up replicas if needed: kubectl scale deployment/user-svc --replicas=5"
  ],
  "lexical_search_query": "user-svc 5xx error rate_limit database deadlock timeout dependency",
  "semantic_search_query": "user service API errors related to rate limiting, database deadlocks, and dependency timeouts during high traffic periods"
}`

	content := fmt.Sprintf("Service: %s\nAlert: %s\nMessages:\n", service, alert)
	for _, msg := range msgs {
		content += msg + "\n"
	}

	chatMessages, inputMessages := createLLMMessages(prompt, content)
	respContent, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, true)
	if err != nil {
		return nil, fmt.Errorf("creating runbook: %w", err)
	}

	slog.DebugContext(ctx, "created runbook", "response", respContent)

	var runbook RunbookResponse
	if err := json.Unmarshal([]byte(respContent), &runbook); err != nil {
		return nil, fmt.Errorf("unmarshaling runbook response: %w", err)
	}

	return &runbook, nil
}

func (c *client) GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error) {
	if c == nil {
		return nil, nil
	}

	inputText := fmt.Sprintf("%s: %s", task, text)
	params := openai.EmbeddingNewParams{
		Model: c.cfg.EmbeddingModel,
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: param.NewOpt(inputText),
		},
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
		Dimensions:     param.NewOpt[int64](768),
	}

	resp, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	// Persist LLM usage (best effort)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Text: inputText,
		},
		dto.LLMOutput{
			Embedding: make([]float32, len(resp.Data[0].Embedding)),
			Usage: &dto.LLMUsageStats{
				PromptTokens: int(resp.Usage.PromptTokens),
				TotalTokens:  int(resp.Usage.TotalTokens),
			},
		},
		c.cfg.EmbeddingModel,
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

	chatMessages, inputMessages := createLLMMessages(prompt, "")
	respMsg, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, true)
	if err != nil {
		return "", "", fmt.Errorf("running JSON mode prompt: %w", err)
	}

	slog.DebugContext(ctx, "ran JSON mode prompt", "response", respMsg)

	if jsonSchema != nil {
		if keyErr, err := jsonSchema.ValidateBytes(ctx, []byte(respMsg)); err != nil || len(keyErr) > 0 {
			return "", respMsg, fmt.Errorf("validating response: %v %w", keyErr, err)
		}
	}
	return respMsg, "", nil
}

func (c *client) ClassifyCommand(ctx context.Context, text string, sampleMessages map[string][]string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a command classifier for a Slack bot named Ratchet that helps reduce operational toil. Your task is to identify the most appropriate command from user input.

Given a message, respond with EXACTLY ONE of these commands (no explanation, just the command name):
- weekly_report (for generating incident/alert reports or summaries for a channel)
- usage_report (for showing bot usage statistics and feedback metrics)
- lookup_documentation (for looking up documentation)
- update_documentation (for updating documentation)
- none (for messages that don't match any supported command)

Examples:`

	// Add examples from sampleMessages
	for cmd, examples := range sampleMessages {
		for _, example := range examples {
			prompt += fmt.Sprintf("\nUser: %q\nResponse: %s\n", example, cmd)
		}
	}

	prompt += `
Classify the following message with ONLY the command name and nothing else:

User: %s
Response:
`

	chatMessages, inputMessages := createLLMMessages(prompt, text)
	content, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.0, false)
	if err != nil {
		return "", fmt.Errorf("classifying command: %w", err)
	}

	return content, nil
}

func (c *client) GenerateDocumentationResponse(ctx context.Context, question string, documents []string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a technical writer for a Slack bot named Ratchet that helps reduce operational toil. Your task is to answer questions about the provided documentation.

	Given a question and a list of documentation documents, respond with a concise answer that is helpful and informative.
	
	The documents are provided as a list of relevant document content (typically in Markdown format).
	
	IMPORTANT INSTRUCTIONS:
	1. Only answer if you are highly confident and your response is primarily derived from the provided documentation.
	2. If you cannot find relevant information in the documents, return "nil".
	3. Keep your answers concise and focused on the question.
	
	Here are some examples:
	
	Example 1:
	Question: "How do I configure the database connection?"
	Documents: [
		"# Configuration\n\nTo configure the database connection, set the DATABASE_URL environment variable in your .env file...", 
		"# Database Setup\n\nThe connection string format should be: postgresql://username:password@hostname:port/database_name"
	]
	Response: "To configure the database connection, set the DATABASE_URL environment variable in your .env file. The format should be: postgresql://username:password@hostname:port/database_name"
	
	Example 2:
	Question: "What is the maximum file size for uploads?"
	Documents: ["# API Documentation\n\nThis document describes the REST API endpoints available."]
	Response: "nil"
	
	Question: %s
	Documents: %s
	
	Response:
	`

	content := fmt.Sprintf(prompt, question, documents)
	chatMessages, inputMessages := createLLMMessages(prompt, content)
	respContent, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, false)
	if err != nil {
		return "", fmt.Errorf("generating documentation response: %w", err)
	}

	if respContent == "nil" {
		return "", nil
	}

	return respContent, nil
}

func (c *client) GenerateDocumentationUpdate(ctx context.Context, doc string, msgs string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a technical writer for a Slack bot named Ratchet that helps reduce operational toil. Your task is to update the provided documentation based on the provided messages.

	IMPORTANT INSTRUCTIONS:
	1. Make minimal changes to the original documentation, preserving its structure and style.
	2. Only update information that is clearly outdated or incorrect based on the messages.
	3. If new information should be added but doesn't fit the current structure, add it as a FAQ item at the end.
	4. Follow the existing document style (formatting, tone, terminology).
	5. Return the complete updated documentation.

	Here are some examples:

	Example 1:
	Documentation: "# Alerts\n\nTo configure alerts, use the /alerts command with the following parameters: --service, --threshold."
	Messages: "The /alerts command now supports a new --priority parameter to set alert priority."
	Updated Documentation: "# Alerts\n\nTo configure alerts, use the /alerts command with the following parameters: --service, --threshold, --priority.\n\n## FAQ\n\n**Q: How do I set the priority of an alert?**\n**A:** Use the --priority parameter with the /alerts command."

	Example 2:
	Documentation: "# Installation\n\nInstall the package using: npm install ratchet-bot"
	Messages: "The npm install command is wrong, it should be npm install @dynoinc/ratchet-bot"
	Updated Documentation: "# Installation\n\nInstall the package using: npm install @dynoinc/ratchet-bot"

	Documentation: %s
	Messages: %s

	Updated Documentation:
	`

	content := fmt.Sprintf(prompt, doc, msgs)
	chatMessages, inputMessages := createLLMMessages(prompt, content)
	respContent, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, false)
	if err != nil {
		return "", fmt.Errorf("generating documentation update: %w", err)
	}

	return respContent, nil
}
