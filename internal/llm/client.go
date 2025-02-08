package llm

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Config struct {
	APIKey         string `envconfig:"API_KEY"`
	URL            string `default:"http://localhost:11434/v1/"`
	Model          string `default:"qwen2.5:7b"`
	EmbeddingModel string `default:"qwen2.5:7b"`
}

type Client struct {
	client *openai.Client
	cfg    Config
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.URL != "http://localhost:11434/v1/" && cfg.APIKey == "" {
		return nil, nil
	}

	client := openai.NewClient(option.WithBaseURL(cfg.URL), option.WithAPIKey(cfg.APIKey))
	_, err := client.Models.Get(ctx, cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("getting model: %w", err)
	}
	_, err = client.Models.Get(ctx, cfg.EmbeddingModel)
	if err != nil {
		return nil, fmt.Errorf("getting embedding model: %w", err)
	}

	return &Client{
		client: client,
		cfg:    cfg,
	}, nil
}

func (c *Client) GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error) {
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

func (c *Client) ClassifyService(ctx context.Context, text string, services []string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are a service classification assistant. Your task is to identify which service from the following list is being referenced in the user's message:

Services: ` + strings.Join(services, ", ") + `

Rules:
- Return EXACTLY one service name from the list above
- If no service matches, return "none"
- Return ONLY the service name, no explanation
- The response must match the exact spelling and case of the service name

Message to classify:
` + text

	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.cfg.Model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleSystem),
				Content: openai.F(any(prompt)),
			},
		}),
		Temperature: openai.F(0.0),
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("classifying service: %w", err)
	}

	slog.DebugContext(ctx, "classified service", "request", params, "response", resp.Choices[0].Message.Content)

	// Clean up response by trimming whitespace and converting to lowercase for comparison
	service := strings.TrimSpace(resp.Choices[0].Message.Content)
	if service == "none" {
		return "", nil
	}

	// Case-sensitive check if service exists in list
	if !slices.Contains(services, service) {
		slog.WarnContext(ctx, "llm returned invalid service name", "service", service)
		return "", nil
	}

	return service, nil
}

func (c *Client) CreateRunbook(ctx context.Context, service string, alert string, msgs []schema.ThreadMessagesV2) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `Create a concise runbook based on the provided incident messages from past triaging. Structure as follows:

**Overview**
- Brief description of the alert and its trigger conditions

**Root Causes**
- Identified causes from past incidents
- Contributing factors

**Resolution Steps**
- Specific troubleshooting steps taken
- Commands used and their outcomes
- Successful resolution actions

RULES:
- Format in Slack-friendly Markdown.
- Include only information explicitly mentioned in the messages.
- Omit sections if no relevant information is available.
- Keep the content focused and specific to what was discussed.
- Do not include generic advice or steps that weren't mentioned in the messages.
`

	content := "Messages:\n"
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
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("creating runbook: %w", err)
	}

	slog.DebugContext(ctx, "created runbook", "request", params, "response", resp.Choices[0].Message.Content)
	return resp.Choices[0].Message.Content, nil
}

func (c *Client) UpdateRunbook(ctx context.Context, runbook schema.IncidentRunbook, msgs []schema.ThreadMessagesV2) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are updating an existing runbook for an incident alert. Review the existing runbook and new messages to make incremental updates.

**Overview**
- Brief description of the alert and its trigger conditions

**Root Causes** 
- Identified causes from past incidents
- Contributing factors

**Resolution Steps**
- Specific troubleshooting steps taken
- Commands used and their outcomes
- Successful resolution actions

RULES:
- Format in Slack-friendly Markdown
- Only add new information from the messages
- Keep existing valid information
- Remove outdated/incorrect information
- Be concise and specific
- Only include information explicitly mentioned`

	content := fmt.Sprintf("Current runbook:\n%s\n\nNew messages:\n", runbook.Attrs.Runbook)
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
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("updating runbook: %w", err)
	}

	slog.DebugContext(ctx, "updated runbook", "request", params, "response", resp.Choices[0].Message.Content)
	return resp.Choices[0].Message.Content, nil
}

func (c *Client) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if c == nil {
		return nil, nil
	}

	params := openai.EmbeddingNewParams{
		Model: openai.F(openai.EmbeddingModel(c.cfg.EmbeddingModel)),
		Input: openai.F(openai.EmbeddingNewParamsInputUnion(
			openai.EmbeddingNewParamsInputArrayOfStrings([]string{text}),
		)),
		EncodingFormat: openai.F(openai.EmbeddingNewParamsEncodingFormatFloat),
		Dimensions:     openai.F(int64(1536)),
	}

	resp, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	r := make([]float32, len(resp.Data[0].Embedding))
	for i, v := range resp.Data[0].Embedding {
		r[i] = float32(v)
	}

	return r[:1536], nil
}
