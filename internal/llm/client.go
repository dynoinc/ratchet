package llm

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type Config struct {
	APIKey string `envconfig:"API_KEY"`
	URL    string `default:"http://localhost:11434/v1/"`
	Model  string `default:"qwen2.5:7b"`
}

type Client struct {
	client *openai.Client
	model  string
}

func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.URL != "http://localhost:11434/v1/" && cfg.APIKey == "" {
		return nil, nil
	}

	client := openai.NewClient(option.WithBaseURL(cfg.URL), option.WithAPIKey(cfg.APIKey))
	model, err := client.Models.Get(ctx, cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("getting model: %w", err)
	}

	return &Client{
		client: client,
		model:  model.ID,
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
		Model: openai.F(openai.ChatModel(c.model)),
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
		Model: openai.F(openai.ChatModel(c.model)),
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
