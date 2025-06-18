package llm

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

	GenerateEmbedding(ctx context.Context, task string, text string) ([]float32, error)
	GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error)
	GenerateDocumentationResponse(ctx context.Context, question string, documents []string) (string, error)
	GenerateDocumentationUpdate(ctx context.Context, doc string, msgs string) (string, error)
	GenerateRunbook(ctx context.Context, service string, alert string, msgs []string) (*RunbookResponse, error)
	RunJSONModePrompt(ctx context.Context, prompt string, schema *jsonschema.Schema) (string, string, error)
	ClassifyCommand(ctx context.Context, text string, sampleMessages map[string][]string) (string, error)
	ClassifyMessage(ctx context.Context, text string, classes map[string]string) (string, string, error)
}

type client struct {
	client openai.Client
	cfg    Config
	db     *pgxpool.Pool
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

		// Persist the usage data
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
		}

		output.Usage = &dto.LLMUsageStats{
			PromptTokens:     int(chatResp.Usage.PromptTokens),
			CompletionTokens: int(chatResp.Usage.CompletionTokens),
			TotalTokens:      int(chatResp.Usage.TotalTokens),
		}

		return output
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

func (c *client) Client() openai.Client {
	return c.client
}

// runChatCompletion is a helper function to run chat completions with common logic
func (c *client) runChatCompletion(
	ctx context.Context,
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
	return content, nil
}

// createLLMMessages creates a slice of message params from system and user content
func createLLMMessages(systemContent string, userContent string) []openai.ChatCompletionMessageParamUnion {
	messages := []openai.ChatCompletionMessageParamUnion{
		{
			OfSystem: &openai.ChatCompletionSystemMessageParam{
				Content: openai.ChatCompletionSystemMessageParamContentUnion{
					OfString: param.NewOpt(systemContent),
				},
			},
		},
	}

	if userContent != "" {
		messages = append(messages, openai.ChatCompletionMessageParamUnion{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: param.NewOpt(userContent),
				},
			},
		})
	}

	return messages
}

func (c *client) GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error) {
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

	Examples:

	Example 1:
	Messages: [["User A: Why does the payment service keep timing out? Seems like the DB connection pool is full again.", "User B: Yeah, seeing that too. Had to restart the pods earlier."]]
	Response:
	*Investigate Payment Service DB Connection Leaks*
	• Recurring timeouts linked to database connection pool exhaustion suggest a potential connection leak requiring investigation and fixing.

	Example 2:
	Messages: [["User C: How do I get logs for the auth service? The runbook link is broken.", "User D: Use 'kubectl logs -l app=auth-svc -n prod'", "User C: Thanks! We should update the runbook."]]
	Response:
	*Update Auth Service Logging Documentation*
	• The runbook for auth service logging is outdated or broken; update it with the correct 'kubectl' command.
	`

	userContent := fmt.Sprintf("Messages:\n%s", messages)
	chatMessages := createLLMMessages(prompt, userContent)

	content, err := c.runChatCompletion(ctx, chatMessages, 0.7, false)
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
- Prioritize extracting verbatim commands and their observed outcomes within the 'resolution_steps' array.
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

	chatMessages := createLLMMessages(prompt, content)
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
	chatMessages := createLLMMessages(prompt, "")
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

func (c *client) ClassifyCommand(ctx context.Context, text string, sampleMessages map[string][]string) (string, error) {
	prompt := `You are a command classifier for a Slack bot named Ratchet that helps reduce operational toil. Your task is to identify the most appropriate command from user input.

Given a message, respond with EXACTLY ONE of these commands (no explanation, just the command name):
- weekly_report (for generating incident/alert reports or summaries for a channel)
- usage_report (for showing bot usage statistics and feedback metrics)
- enable_auto_doc_reply (for enabling auto doc reply)
- disable_auto_doc_reply (for disabling auto doc reply)
- lookup_documentation (for looking up documentation)
- update_documentation (for updating documentation)
- none (for messages that don't match any supported command)

Do not add any conversational text, explanation, or formatting; output *only* the single command name.

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

	chatMessages := createLLMMessages(prompt, text)
	content, err := c.runChatCompletion(ctx, chatMessages, 0.0, false)
	if err != nil {
		return "", fmt.Errorf("classifying command: %w", err)
	}

	return content, nil
}

func (c *client) ClassifyMessage(ctx context.Context, text string, classes map[string]string) (string, string, error) {
	prompt := `You are an expert at classifying Slack messages in team help and operations channels. Your task is to analyze messages and categorize them accurately.

CLASSIFICATION PROCESS:
1. Read the message carefully and identify the main intent
2. Consider the context (team/ops channel communication)
3. Look for key indicators like question words, action requests, urgency markers
4. Match against the provided classes based on the primary purpose

AVAILABLE CLASSES:`

	for class, description := range classes {
		prompt += fmt.Sprintf("\n- %s: %s", class, description)
	}

	prompt += `

CLASSIFICATION EXAMPLES:

Help Request Examples:
- "How do I restart the payment service?"
- "Getting 500 errors on the API, anyone know what's wrong?"
- "Can someone help me debug this database connection issue?"

Production Change Examples:
- "Need to deploy the hotfix to production ASAP"
- "Can we update the config for the auth service?"
- "Planning to scale up the workers, any objections?"

Code Review Examples:
- "PR ready for review: https://github.com/org/repo/pull/123"
- "Can someone take a look at my changes?"
- "Need eyes on this refactor before merging"

Incident Report Examples:
- "ALERT: Payment service is down"
- "Users reporting login failures across all regions"
- "Database CPU is at 95%, investigating"

Status Update Examples:
- "Deployment completed successfully"
- "Fixed the issue, monitoring for any further problems"
- "Services are back to normal"

Other Examples:
- "Thanks for the help!"
- "Good morning team"
- "Meeting in 5 minutes"

INSTRUCTIONS:
- Analyze the message's PRIMARY intent and purpose
- Choose the MOST SPECIFIC class that fits
- If multiple classes could apply, pick the one that represents the main action needed
- Use "other" only for messages that clearly don't fit any provided class
- Provide a concise reason explaining your classification choice

Message to classify: "%s"

Return a JSON object with "class" and "reason" fields:`

	chatMessages := createLLMMessages(prompt, text)
	respContent, err := c.runChatCompletion(ctx, chatMessages, 0.1, true)
	if err != nil {
		return "", "", fmt.Errorf("classifying message: %w", err)
	}

	var resp struct {
		Class  string `json:"class"`
		Reason string `json:"reason"`
	}

	if err := json.Unmarshal([]byte(respContent), &resp); err != nil {
		return "", "", fmt.Errorf("unmarshaling response: %w", err)
	}

	if resp.Class == "" {
		return "", "", fmt.Errorf("no class found in response: %s", respContent)
	}

	return resp.Class, resp.Reason, nil
}

func (c *client) GenerateDocumentationResponse(ctx context.Context, question string, documents []string) (string, error) {
	prompt := `You are a technical writer for a Slack bot named Ratchet that helps reduce operational toil. Your task is to answer questions based *strictly* on the provided documentation excerpts.

Given a question and a list of relevant documentation excerpts (typically in Markdown format), follow these steps:

1.  **Analyze:** Carefully read the question and all provided documentation excerpts.
2.  **Evaluate:** Determine if the excerpts contain sufficient information to *directly* and *confidently* answer the question.
3.  **Respond:**
    *   **If YES:** Construct a concise answer derived *solely* from the information present in the excerpts. Quote relevant parts directly when possible, but do NOT simply paste the entire documentation verbatim. Synthesize and present only the relevant information that answers the question. Do NOT add any external knowledge or make inferences beyond what is explicitly stated.
    *   **If NO:** Respond *only* with the following exact phrase: "I couldn't find information about this in our documentation. If someone answers your question, please consider updating our docs by using the command '@ratchet update docs for <topic>'."

**IMPORTANT INSTRUCTIONS:**
*   Prioritize accuracy and adherence to the provided documentation above all else.
*   If the documentation mentions related topics but doesn't answer the *specific* question asked, use the fallback response.
*   Keep your answers concise and focused on the question.
*   Never repeat the entire documentation verbatim - extract and synthesize only the relevant parts.

**Examples:**

Example 1 (Sufficient Information):
Question: "How do I configure the database connection?"
Documents: [
	"# Configuration\n\nTo configure the database connection, set the DATABASE_URL environment variable in your .env file...", 
	"# Database Setup\n\nThe connection string format should be: postgresql://username:password@hostname:port/database_name"
]
Response: "To configure the database connection, set the DATABASE_URL environment variable in your .env file. The format should be: postgresql://username:password@hostname:port/database_name"

Example 2 (Insufficient Information):
Question: "What is the maximum file size for uploads?"
Documents: ["# API Documentation\n\nThis document describes the REST API endpoints available."]
Response: "I couldn't find information about this in our documentation. If someone answers your question, please consider updating our docs by using the command '@ratchet update docs for <topic>'."

Example 3 (Related but Not Specific Information):
Question: "How do I reset a user's password via the API?"
Documents: ["# User Management API\n\nProvides endpoints for creating, updating, and deleting users. The update endpoint allows changing user attributes like email and roles."]
Response: "I couldn't find information about this in our documentation. If someone answers your question, please consider updating our docs by using the command '@ratchet update docs for <topic>'."

**Now, answer the following:**

Question: %s
Documents: %s

Response:
`

	content := fmt.Sprintf(prompt, question, documents)
	chatMessages := createLLMMessages(prompt, content)
	respContent, err := c.runChatCompletion(ctx, chatMessages, 0.2, false)
	if err != nil {
		return "", fmt.Errorf("generating documentation response: %w", err)
	}

	return respContent, nil
}

func (c *client) GenerateDocumentationUpdate(ctx context.Context, doc string, msgs string) (string, error) {
	systemPrompt := `You are a technical writer for a Slack bot named Ratchet that helps reduce operational toil. Your task is to update the provided documentation based on the technical details discussed in the provided Slack messages.

	IMPORTANT INSTRUCTIONS:
	1.  **Focus on Technical Accuracy:** Your primary goal is to incorporate relevant technical facts, procedures, configurations, code snippets, or corrections mentioned in the messages into the documentation.
	2.  **Preserve & Minimize:** Edit the original documentation minimally. Preserve its existing structure, formatting, tone, and terminology. Avoid unnecessary changes or removing content unless the messages explicitly state it's wrong or deprecated.
	3.  **Integrate First:** Modify the documentation *only* based on information clearly present in the provided messages. Integrate new technical details or corrections smoothly into the most relevant existing sections. Improve clarity and add detail where the messages provide it.
	4.  **Use FAQs Sparingly:** If a message thread clearly discusses a *specific technical question* and provides a *clear answer* that doesn't logically fit into the existing document structure, *then* add it as a concise Q&A item under an "## FAQ" section at the end. Do *not* add generic FAQs or summarize the conversation; focus only on distinct, technical Q&A pairs derived directly from the messages.
	5.  **Source Constraint:** Do *not* add any information that is not present in the original documentation or the provided messages. Stick strictly to the provided text.
	6.  **Handle Irrelevance:** If the messages are not relevant to the technical content of the original documentation (e.g., they discuss a completely different topic or are just chit-chat), return the original documentation *exactly* as provided, without any changes.
	7.  **Output:** Return the *complete* updated documentation (or the unchanged original if step 6 applies).

	Here are some examples:

	Example 1 (Integration):
	Original Documentation: "# Alerts\n\nTo configure alerts, use the /alerts command with the following parameters: --service, --threshold."
	Messages: "Hey team, remember the /alerts command now supports a new --priority parameter to set alert priority (low, medium, high). Default is medium."
	Updated Documentation: "# Alerts\n\nTo configure alerts, use the /alerts command with the following parameters:\n* ` + "`" + `--service` + "`" + `: The name of the service.\n* ` + "`" + `--threshold` + "`" + `: The alert threshold.\n* ` + "`" + `--priority` + "`" + `: (Optional) Set the alert priority (low, medium, high). Defaults to medium."

	Example 2 (Correction):
	Original Documentation: "# Installation\n\nInstall the package using: npm install ratchet-bot"
	Messages: "The npm install command in the docs is wrong, it should be npm install @dynoinc/ratchet-bot"
	Updated Documentation: "# Installation\n\nInstall the package using: npm install @dynoinc/ratchet-bot"

	Example 3 (Specific FAQ):
	Original Documentation: "# Database Setup\n\nConnect using the DATABASE_URL environment variable."
	Messages: "Q: What's the timeout for database connections?\nA: It's configurable via the DB_TIMEOUT_MS env var, defaults to 5000ms."
	Updated Documentation: "# Database Setup\n\nConnect using the DATABASE_URL environment variable.\n\n## FAQ\n\n**Q: How is the database connection timeout configured?**\n**A:** Use the ` + "`" + `DB_TIMEOUT_MS` + "`" + ` environment variable. It defaults to 5000 milliseconds."

	Example 4 (Irrelevant):
	Original Documentation: "# API Keys\n\nGenerate API keys in the settings panel."
	Messages: "Anyone seen my stapler?"
	Updated Documentation: "# API Keys\n\nGenerate API keys in the settings panel."
	`

	userContent := fmt.Sprintf(`Here is the original documentation and the relevant messages. Please update the documentation according to the instructions provided in the system prompt. Provide only the complete updated documentation in your response.

**Original Documentation:**
---
%s
---

**Messages:**
---
%s
---`, doc, msgs)

	chatMessages := createLLMMessages(systemPrompt, userContent)
	respContent, err := c.runChatCompletion(ctx, chatMessages, 0.7, false)
	if err != nil {
		return "", fmt.Errorf("generating documentation update: %w", err)
	}

	return respContent, nil
}
