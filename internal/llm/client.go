package llm

import (
	"context"
	"fmt"
	"log/slog"

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

func (c *Client) GenerateChannelSuggestions(ctx context.Context, messages []string) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `You are tech-lead of the a software engineering team, who is looking for opportunities for how to make
	life of your users better. By taking a look at help slack messages users have posted, you figure out actionable changes
	your team can make to improve future support requests. You explain with example the reasons why some actions should be taken.

	Keep each suggestion limited to a title and a concise bullet point. Format using markdown for posting to slack.
	`

	params := openai.ChatCompletionNewParams{
		Model: openai.F(openai.ChatModel(c.model)),
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleDeveloper),
				Content: openai.F(any(prompt)),
			},
			openai.ChatCompletionMessageParam{
				Role:    openai.F(openai.ChatCompletionMessageParamRoleUser),
				Content: openai.F(any(fmt.Sprintf("Messages:\n%s", messages))),
			},
		}),
	}

	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("generating suggestions: %w", err)
	}

	slog.DebugContext(ctx, "generated suggestions", "request", params, "response", resp.Choices[0].Message.Content)

	return resp.Choices[0].Message.Content, nil
}
