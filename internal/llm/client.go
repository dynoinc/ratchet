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
	"time"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/kelseyhightower/envconfig"
	ollama "github.com/ollama/ollama/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/qri-io/jsonschema"
	"github.com/riverqueue/river"
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
	RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, error)
	ClassifyCommand(ctx context.Context, text string) (string, error)
	SetRiverClient(riverClient *river.Client[pgx.Tx])
}

// UsageRecord represents a record of LLM usage
type UsageRecord struct {
	ID               uuid.UUID       `json:"id,omitempty"`
	CreatedAt        time.Time       `json:"created_at,omitempty"`
	Model            string          `json:"model"`
	OperationType    string          `json:"operation_type"`
	PromptText       string          `json:"prompt_text"`
	CompletionText   string          `json:"completion_text,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	TotalTokens      int             `json:"total_tokens,omitempty"`
	LatencyMs        int             `json:"latency_ms,omitempty"`
	Status           string          `json:"status"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

// Operation types for the UsageRecord.OperationType field
const (
	OpTypeCompletion     = "completion"
	OpTypeEmbedding      = "embedding"
	OpTypeClassification = "classification"
	OpTypeRunbook        = "runbook"
	OpTypeJSONPrompt     = "json_prompt"
)

// Status types for the UsageRecord.Status field
const (
	StatusSuccess = "success"
	StatusError   = "error"
)

type client struct {
	client      *openai.Client
	cfg         Config
	riverClient *river.Client[pgx.Tx]
}

// RecordLLMUsageParams contains the parameters for recording LLM usage
type RecordLLMUsageParams struct {
	Model            string          `json:"model"`
	OperationType    string          `json:"operation_type"`
	PromptText       string          `json:"prompt_text"`
	CompletionText   string          `json:"completion_text,omitempty"`
	PromptTokens     int32           `json:"prompt_tokens,omitempty"`
	CompletionTokens int32           `json:"completion_tokens,omitempty"`
	TotalTokens      int32           `json:"total_tokens,omitempty"`
	LatencyMs        int32           `json:"latency_ms,omitempty"`
	Status           string          `json:"status"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
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

	userContent := fmt.Sprintf("Messages:\n%s", messages)

	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.cfg.Model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleSystem),
				Content: openai.F(any(prompt)),
			},
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleUser),
				Content: openai.F(any(userContent)),
			},
		}),
		Temperature: openai.F(0.7),
	}

	startTime := time.Now()
	resp, err := c.client.Chat.Completions.New(ctx, params)
	latencyMs := int(time.Since(startTime).Milliseconds())

	// Record usage data
	usage := &UsageRecord{
		Model:         string(c.cfg.Model),
		OperationType: OpTypeCompletion,
		PromptText:    fmt.Sprintf("%s\n%s", prompt, userContent),
		LatencyMs:     latencyMs,
	}

	if err != nil {
		usage.Status = StatusError
		usage.ErrorMessage = err.Error()
		if c.riverClient != nil {
			c.recordUsage(ctx, usage)
		}
		return "", fmt.Errorf("generating suggestions: %w", err)
	}

	content := resp.Choices[0].Message.Content
	usage.Status = StatusSuccess
	usage.CompletionText = content

	// Add token usage if available
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)

	if promptTokens > 0 || completionTokens > 0 || totalTokens > 0 {
		usage.PromptTokens = promptTokens
		usage.CompletionTokens = completionTokens
		usage.TotalTokens = totalTokens
	}

	// Try to record the usage
	if c.riverClient != nil {
		c.recordUsage(ctx, usage)
	}

	slog.DebugContext(ctx, "generated suggestions", "request", params, "response", content)

	return content, nil
}

type RunbookResponse struct {
	AlertOverview        string   `json:"alert_overview"`
	HistoricalRootCauses []string `json:"historical_root_causes"`
	ResolutionSteps      []string `json:"resolution_steps"`
	SearchQuery          string   `json:"search_query"`
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

	startTime := time.Now()
	resp, err := c.client.Chat.Completions.New(ctx, params)
	latencyMs := int(time.Since(startTime).Milliseconds())

	// Prepare metadata with additional info about the runbook request
	metadataObj := map[string]interface{}{
		"service": service,
		"alert":   alert,
	}
	metadataJSON, _ := json.Marshal(metadataObj)

	// Record usage data
	usage := &UsageRecord{
		Model:         string(c.cfg.Model),
		OperationType: OpTypeRunbook,
		PromptText:    fmt.Sprintf("%s\n%s", prompt, content),
		LatencyMs:     latencyMs,
		Metadata:      metadataJSON,
	}

	if err != nil {
		usage.Status = StatusError
		usage.ErrorMessage = err.Error()
		if c.riverClient != nil {
			c.recordUsage(ctx, usage)
		}
		return nil, fmt.Errorf("creating runbook: %w", err)
	}

	jsonContent := resp.Choices[0].Message.Content
	usage.Status = StatusSuccess
	usage.CompletionText = jsonContent

	// Add token usage if available
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)

	if promptTokens > 0 || completionTokens > 0 || totalTokens > 0 {
		usage.PromptTokens = promptTokens
		usage.CompletionTokens = completionTokens
		usage.TotalTokens = totalTokens
	}

	// Try to record the usage
	if c.riverClient != nil {
		c.recordUsage(ctx, usage)
	}

	slog.DebugContext(ctx, "created runbook", "request", params, "response", jsonContent)

	var runbook RunbookResponse
	if err := json.Unmarshal([]byte(jsonContent), &runbook); err != nil {
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
		Model: openai.F(openai.EmbeddingModel(c.cfg.EmbeddingModel)),
		Input: openai.F(openai.EmbeddingNewParamsInputUnion(
			openai.EmbeddingNewParamsInputArrayOfStrings([]string{inputText}),
		)),
		EncodingFormat: openai.F(openai.EmbeddingNewParamsEncodingFormatFloat),
		Dimensions:     openai.F(int64(768)),
	}

	startTime := time.Now()
	resp, err := c.client.Embeddings.New(ctx, params)
	latencyMs := int(time.Since(startTime).Milliseconds())

	// Record usage data
	usage := &UsageRecord{
		Model:         string(c.cfg.EmbeddingModel),
		OperationType: OpTypeEmbedding,
		PromptText:    inputText,
		LatencyMs:     latencyMs,
	}

	if err != nil {
		usage.Status = StatusError
		usage.ErrorMessage = err.Error()
		if c.riverClient != nil {
			c.recordUsage(ctx, usage)
		}
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	usage.Status = StatusSuccess

	// Embedding models report tokens differently - there's only prompt tokens
	promptTokens := int(resp.Usage.PromptTokens)
	totalTokens := int(resp.Usage.TotalTokens)

	if promptTokens > 0 || totalTokens > 0 {
		usage.PromptTokens = promptTokens
		usage.TotalTokens = totalTokens
	}

	// Try to record the usage
	if c.riverClient != nil {
		c.recordUsage(ctx, usage)
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

	startTime := time.Now()
	resp, err := c.client.Chat.Completions.New(ctx, params)
	latencyMs := int(time.Since(startTime).Milliseconds())

	// Record usage data
	var schemaStr string
	if jsonSchema != nil {
		schemaBytes, _ := json.Marshal(jsonSchema)
		schemaStr = string(schemaBytes)
	}
	metadataObj := map[string]interface{}{
		"schema_provided": jsonSchema != nil,
	}
	metadataJSON, _ := json.Marshal(metadataObj)

	usage := &UsageRecord{
		Model:         string(c.cfg.Model),
		OperationType: OpTypeJSONPrompt,
		PromptText:    fmt.Sprintf("%s\nSchema: %s", prompt, schemaStr),
		LatencyMs:     latencyMs,
		Metadata:      metadataJSON,
	}

	if err != nil {
		usage.Status = StatusError
		usage.ErrorMessage = err.Error()
		if c.riverClient != nil {
			c.recordUsage(ctx, usage)
		}
		return "", fmt.Errorf("running JSON mode prompt: %w", err)
	}

	respMsg := resp.Choices[0].Message.Content
	usage.Status = StatusSuccess
	usage.CompletionText = respMsg

	// Add token usage if available
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)

	if promptTokens > 0 || completionTokens > 0 || totalTokens > 0 {
		usage.PromptTokens = promptTokens
		usage.CompletionTokens = completionTokens
		usage.TotalTokens = totalTokens
	}

	// Try to record the usage
	if c.riverClient != nil {
		c.recordUsage(ctx, usage)
	}

	slog.DebugContext(ctx, "ran JSON mode prompt", "request", params, "response", respMsg)

	if jsonSchema != nil {
		if keyErr, err := jsonSchema.ValidateBytes(ctx, []byte(respMsg)); err != nil || len(keyErr) > 0 {
			return "", fmt.Errorf("validating response: %v %w", keyErr, err)
		}
	}
	return respMsg, nil
}

func (c *client) ClassifyCommand(ctx context.Context, text string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a command classifier that identifies the most appropriate command from user input. 
Given a message, respond with exactly one of these commands:
- weekly_report
- usage_report
- leave_channel
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

User: "leave channel"
Response: leave_channel

User: "get out of this channel"
Response: leave_channel

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

	startTime := time.Now()
	resp, err := c.client.Chat.Completions.New(ctx, params)
	latencyMs := int(time.Since(startTime).Milliseconds())

	// Record the usage data
	usage := &UsageRecord{
		Model:         string(c.cfg.Model),
		OperationType: OpTypeClassification,
		PromptText:    fmt.Sprintf("%s\n%s", prompt, text),
		LatencyMs:     latencyMs,
	}

	if err != nil {
		usage.Status = StatusError
		usage.ErrorMessage = err.Error()
		// Try to record the error, but don't return an error from this function
		if c.riverClient != nil {
			c.recordUsage(ctx, usage)
		}
		return "", fmt.Errorf("classifying command: %w", err)
	}

	content := resp.Choices[0].Message.Content
	usage.Status = StatusSuccess
	usage.CompletionText = content

	// Add token usage if available
	promptTokens := int(resp.Usage.PromptTokens)
	completionTokens := int(resp.Usage.CompletionTokens)
	totalTokens := int(resp.Usage.TotalTokens)

	if promptTokens > 0 || completionTokens > 0 || totalTokens > 0 {
		usage.PromptTokens = promptTokens
		usage.CompletionTokens = completionTokens
		usage.TotalTokens = totalTokens
	}

	// Try to record the usage, but don't return an error from this function
	if c.riverClient != nil {
		c.recordUsage(ctx, usage)
	}

	return content, nil
}

// SetRiverClient sets the River client for queueing usage records
func (c *client) SetRiverClient(riverClient *river.Client[pgx.Tx]) {
	c.riverClient = riverClient
}

// recordUsage is a helper method to queue usage records to River
func (c *client) recordUsage(ctx context.Context, usage *UsageRecord) {
	if c == nil || c.riverClient == nil {
		return
	}

	// Convert to worker args
	args := background.LLMUsageRecordWorkerArgs{
		ID:               usage.ID,
		CreatedAt:        usage.CreatedAt,
		Model:            usage.Model,
		OperationType:    usage.OperationType,
		PromptText:       usage.PromptText,
		CompletionText:   usage.CompletionText,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		TotalTokens:      usage.TotalTokens,
		LatencyMs:        usage.LatencyMs,
		Status:           usage.Status,
		ErrorMessage:     usage.ErrorMessage,
		Metadata:         usage.Metadata,
	}

	// Insert job into River queue
	_, err := c.riverClient.Insert(ctx, args, nil)
	if err != nil {
		slog.ErrorContext(ctx, "failed to queue LLM usage record", "error", err)
	}
}
