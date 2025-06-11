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
	APIKey          string `envconfig:"API_KEY"`
	URL             string `default:"http://localhost:11434/v1/"`
	Model           string `default:"qwen2.5:7b"`
	EmbeddingModel  string `split_words:"true" default:"nomic-embed-text"`
	ToolsConfigFile string `split_words:"true"`
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
	ProccessDeploymentOps(ctx context.Context, text string) (string, error)
}

type client struct {
	client    openai.Client
	cfg       Config
	db        *pgxpool.Pool
	toolFiles *ToolFiles
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

	tools, err := NewToolsInit(cfg.ToolsConfigFile)
	if err != nil {
		return nil, fmt.Errorf("loading tools: %w", err)
	}
	return &client{
		client:    openaiClient,
		cfg:       cfg,
		db:        db,
		toolFiles: tools,
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
	maxRounds int, // maximum number of queries to llm (e.g. 1 if no tools)
	jsonMode bool,
) (string, error) {
	if c == nil {
		return "", nil
	}

	tools, toolToBinMap, err := c.getToolsForAllFiles(ctx)
	if err != nil {
		return "", fmt.Errorf("getting tools: %w", err)
	}

	params := openai.ChatCompletionNewParams{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: param.NewOpt(temperature),
		Tools:       tools,
	}

	if jsonMode {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	}
	var totalUsage dto.LLMUsageStats
	var finalContent string
	var round int

	for round = 0; round < maxRounds; round++ {
		resp, err := c.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("generating response: %w", err)
		}

		message := resp.Choices[0].Message

		// Accumulate usage after each step to log afterwards
		totalUsage.PromptTokens += int(resp.Usage.PromptTokens)
		totalUsage.CompletionTokens += int(resp.Usage.CompletionTokens)
		totalUsage.TotalTokens += int(resp.Usage.TotalTokens)

		if len(message.ToolCalls) == 0 {
			finalContent = message.Content
			break
		}

		// Handle tool calls
		resultsByCallID, err := c.handleToolCalls(ctx, toolToBinMap, message.ToolCalls)
		if err != nil {
			return "", fmt.Errorf("performing tool call: %w", err)
		}

		params.Messages = append(params.Messages, message.ToParam())

		for callID, result := range resultsByCallID {
			params.Messages = append(params.Messages, openai.ToolMessage(result, callID))
		}
	}
	if round >= maxRounds {
		finalContent = fmt.Sprintf("Unable to provide an answer because reached maximum number of rounds (%d).", maxRounds)
	}
	// Persist LLM usage (only keep track of final result and not intermediate steps)
	c.persistLLMUsage(ctx,
		dto.LLMInput{
			Messages: inputMessages,
		},
		dto.LLMOutput{
			Content: finalContent,
			Usage:   &totalUsage,
		},
		c.cfg.Model,
	)
	return finalContent, nil
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
	chatMessages, inputMessages := createLLMMessages(prompt, userContent)

	content, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, 1, false)
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

	chatMessages, inputMessages := createLLMMessages(prompt, content)
	respContent, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, 1, true)
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
	respMsg, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, 1, true)
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
- enable_auto_doc_reply (for enabling auto doc reply)
- disable_auto_doc_reply (for disabling auto doc reply)
- lookup_documentation (for looking up documentation)
- update_documentation (for updating documentation)
- deployment_ops (relating to the deployment lifecycle including deployment queries, adjusting emergency capacity (ecap), rolling back, etc)
- none (for messages that don't match any supported command)

Do not use any tools or attempt to answer the user's query.
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

	chatMessages, inputMessages := createLLMMessages(prompt, text)
	content, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.0, 1, false)
	if err != nil {
		return "", fmt.Errorf("classifying command: %w", err)
	}

	return content, nil
}

func (c *client) ProccessDeploymentOps(ctx context.Context, user_content string) (string, error) {
	if c == nil {
		return "", nil
	}
	sys_prompt := `You are a dev-ops expert for a Slack bot named Ratchet that helps reduce operational toil. Your task is to succesfully use the tools provided to produce an answer to the given query. 

Tool calling guidelines:
- Before calling any tool, you must first **extract, list, and verify all argument values** (e.g., project name, service, team) from the user's query. For example, if the user queries about some project, list all projects.
  - For example, if a user references a specific project or service, you must first list all available projects or services (depending on what the user asked for) and confirm a match before passing any value to a tool.
  - It's okay to be flexible (e.g. match testprojectt with test_project)1

Instructions for handling deployment queries:
- Always include the configSha of each deployment in the response, unless the user specifically requests that it be omitted.

If an answer can not be determined or information is insufficient, please say so clearly.`

	chatMessages, inputMessages := createLLMMessages(sys_prompt, user_content)
	content, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.0, 10, false)
	if err != nil {
		return "", fmt.Errorf("processing deployment ops: %w", err)
	}

	return content, nil
}

func (c *client) GenerateDocumentationResponse(ctx context.Context, question string, documents []string) (string, error) {
	if c == nil {
		return "", nil
	}

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
	chatMessages, inputMessages := createLLMMessages(prompt, content)
	respContent, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.2, 1, false)
	if err != nil {
		return "", fmt.Errorf("generating documentation response: %w", err)
	}

	return respContent, nil
}

func (c *client) GenerateDocumentationUpdate(ctx context.Context, doc string, msgs string) (string, error) {
	if c == nil {
		return "", nil
	}

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

	chatMessages, inputMessages := createLLMMessages(systemPrompt, userContent)
	respContent, err := c.runChatCompletion(ctx, inputMessages, chatMessages, 0.7, 1, false)
	if err != nil {
		return "", fmt.Errorf("generating documentation update: %w", err)
	}

	return respContent, nil
}
