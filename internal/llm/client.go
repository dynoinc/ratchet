package llm

//go:generate go tool mockgen -destination=mocks/mock_client.go -package=mocks -source=client.go Client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

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
	Client() openai.Client
	Model() string

	GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error)
	GenerateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error)
	RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, string, error)
}

type client struct {
	client openai.Client
	cfg    Config
}

func persistLLMUsageMiddleware(db *pgxpool.Pool) option.Middleware {
	if db == nil {
		return option.Middleware(func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
			return next(req)
		})
	}

	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		var input dto.LLMInput
		var model string

		// Extract input data from request
		if req.Body != nil && req.Method == "POST" {
			// Read the request body
			reqBody, err := io.ReadAll(req.Body)
			if err == nil {
				// Restore the body for the actual request
				req.Body = io.NopCloser(bytes.NewBuffer(reqBody))

				// Parse the request to extract model and input
				model, input = extractInputFromRequest(req.URL.Path, reqBody)
			}
		}

		// Make the actual request
		resp, err := next(req)
		if err != nil {
			return resp, err
		}

		// Extract output data from response
		var output dto.LLMOutput
		if resp != nil && resp.Body != nil {
			// Read the response body
			respBody, readErr := io.ReadAll(resp.Body)
			if readErr == nil {
				// Restore the body for the caller
				resp.Body = io.NopCloser(bytes.NewBuffer(respBody))

				// Parse the response to extract output
				output = extractOutputFromResponse(resp.StatusCode, respBody)
			}
		}

		params := schema.AddLLMUsageParams{
			Input:  input,
			Output: output,
			Model:  model,
		}

		if _, persistErr := schema.New(db).AddLLMUsage(req.Context(), params); persistErr != nil {
			slog.WarnContext(req.Context(), "failed to persist LLM usage", "error", persistErr)
		}

		return resp, err
	}
}

func extractInputFromRequest(path string, body []byte) (string, dto.LLMInput) {
	var input dto.LLMInput
	var model string

	// Parse different OpenAI API endpoints
	if strings.Contains(path, "/chat/completions") {
		var chatParams openai.ChatCompletionNewParams
		if err := json.Unmarshal(body, &chatParams); err == nil {
			model = chatParams.Model

			// Convert messages
			input.Messages = make([]dto.LLMMessage, len(chatParams.Messages))
			for i, msg := range chatParams.Messages {
				// Handle different message types
				if msg.OfSystem != nil && msg.OfSystem.Content.OfString.Valid() {
					input.Messages[i] = dto.LLMMessage{
						Role:    "system",
						Content: msg.OfSystem.Content.OfString.Value,
					}
				} else if msg.OfUser != nil && msg.OfUser.Content.OfString.Valid() {
					input.Messages[i] = dto.LLMMessage{
						Role:    "user",
						Content: msg.OfUser.Content.OfString.Value,
					}
				} else if msg.OfAssistant != nil && msg.OfAssistant.Content.OfString.Valid() {
					input.Messages[i] = dto.LLMMessage{
						Role:    "assistant",
						Content: msg.OfAssistant.Content.OfString.Value,
					}
				}
			}

			// Add parameters
			input.Parameters = make(map[string]interface{})
			if chatParams.Temperature.Valid() {
				input.Parameters["temperature"] = chatParams.Temperature.Value
			}
			if chatParams.MaxTokens.Valid() {
				input.Parameters["max_tokens"] = chatParams.MaxTokens.Value
			}
		}
	} else if strings.Contains(path, "/embeddings") {
		var embeddingParams openai.EmbeddingNewParams
		if err := json.Unmarshal(body, &embeddingParams); err == nil {
			model = embeddingParams.Model

			// Handle different input types
			if embeddingParams.Input.OfString.Valid() {
				input.Text = embeddingParams.Input.OfString.Value
			} else if len(embeddingParams.Input.OfArrayOfStrings) > 0 {
				// For array inputs, join them
				input.Text = strings.Join(embeddingParams.Input.OfArrayOfStrings, " ")
			}
		}
	} else if strings.Contains(path, "/models") {
		pathParts := strings.Split(path, "/")
		if len(pathParts) >= 3 {
			model = pathParts[len(pathParts)-1]
		}
	} else {
		slog.WarnContext(context.Background(), "unknown request", "path", path, "body", string(body))
	}

	return model, input
}

func extractOutputFromResponse(statusCode int, body []byte) dto.LLMOutput {
	var output dto.LLMOutput

	if statusCode != 200 {
		output.Error = string(body)
		return output
	}

	// Try to parse as ChatCompletion response first
	var chatResp openai.ChatCompletion
	if err := json.Unmarshal(body, &chatResp); err == nil {
		if len(chatResp.Choices) > 0 {
			output.Content = chatResp.Choices[0].Message.Content
			output.Usage = &dto.LLMUsageStats{
				PromptTokens:     int(chatResp.Usage.PromptTokens),
				CompletionTokens: int(chatResp.Usage.CompletionTokens),
				TotalTokens:      int(chatResp.Usage.TotalTokens),
			}
			return output
		}
		// If no choices, do not treat as chat completion, continue to next parser
	}

	// Try to parse as Embedding response
	var embeddingResp openai.CreateEmbeddingResponse
	if err := json.Unmarshal(body, &embeddingResp); err == nil && len(embeddingResp.Data) > 0 {
		output.Embedding = make([]float32, len(embeddingResp.Data[0].Embedding))
		for i, v := range embeddingResp.Data[0].Embedding {
			output.Embedding[i] = float32(v)
		}

		output.Usage = &dto.LLMUsageStats{
			PromptTokens: int(embeddingResp.Usage.PromptTokens),
			TotalTokens:  int(embeddingResp.Usage.TotalTokens),
		}

		return output
	}

	// Try to parse as Model response
	var modelResp openai.Model
	if err := json.Unmarshal(body, &modelResp); err == nil {
		output.Content = fmt.Sprintf("Model: %s, Created: %d, OwnedBy: %s",
			modelResp.ID, modelResp.Created, modelResp.OwnedBy)
		return output
	}

	output.Error = "Unknown response format: " + string(body)
	return output
}

func New(ctx context.Context, cfg Config, db *pgxpool.Pool) (Client, error) {
	if cfg.URL != "http://localhost:11434/v1/" && cfg.APIKey == "" {
		return nil, nil
	}

	openaiClient := openai.NewClient(
		option.WithBaseURL(cfg.URL),
		option.WithAPIKey(cfg.APIKey),
		option.WithMiddleware(persistLLMUsageMiddleware(db)),
	)

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

func (c *client) Client() openai.Client {
	return c.client
}

func (c *client) Model() string {
	return c.cfg.Model
}

// runChatCompletion is a helper function to run chat completions with common logic
func (c *client) runChatCompletion(
	ctx context.Context,
	messages []openai.ChatCompletionMessageParamUnion,
	temperature float64,
	jsonMode bool,
) (string, error) {
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
	return content, nil
}

type RunbookResponse struct {
	AlertOverview        string   `json:"alert_overview"`
	HistoricalRootCauses []string `json:"historical_root_causes"`
	ResolutionSteps      []string `json:"resolution_steps"`
}

func (c *client) GenerateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error) {
	prompt := `Create a structured runbook based on the provided service, alert, and incident messages. Return a JSON object with these exact fields:

{
  "alert_overview": "Brief description of the alert and what triggers it (2-3 sentences)",
  "historical_root_causes": ["List of specific causes from past incidents"],
  "resolution_steps": ["Concrete troubleshooting steps that were successful, with commands and outcomes"],
}

RULES:
- Include only information explicitly mentioned in the messages
- Keep content specific, technical, and actionable
- Focus on technical details, error patterns, and resolution steps
- Use bullet points for clarity in the JSON arrays
- Prioritize commands, error codes, and specific metrics when available
- Prioritize extracting verbatim commands and their observed outcomes within the 'resolution_steps' array.
- Return empty arrays if no relevant information exists for a section

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
}`

	content := fmt.Sprintf("Service: %s\nAlert: %s\nMessages:\n", service, alert)
	for _, msg := range msgs {
		content += msg + "\n"
	}

	chatMessages := []openai.ChatCompletionMessageParamUnion{
		{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: param.NewOpt(prompt),
				},
			},
		},
		{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: param.NewOpt(content),
				},
			},
		},
	}
	respContent, err := c.runChatCompletion(ctx, chatMessages, 0.7, true)
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

	r := make([]float32, len(resp.Data[0].Embedding))
	for i, v := range resp.Data[0].Embedding {
		r[i] = float32(v)
	}

	return r, nil
}

// RunJSONModePrompt runs a JSON mode prompt and validates the response against the provided JSON schema.
// It returns the response and the raw response message (returned if the response is not valid).
func (c *client) RunJSONModePrompt(ctx context.Context, prompt string, jsonSchema *jsonschema.Schema) (string, string, error) {
	chatMessages := []openai.ChatCompletionMessageParamUnion{
		{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: param.NewOpt(prompt),
				},
			},
		},
	}
	respMsg, err := c.runChatCompletion(ctx, chatMessages, 0.7, true)
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
