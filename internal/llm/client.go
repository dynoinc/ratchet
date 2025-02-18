package llm

import (
	"context"
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
	GenerateChannelSuggestions(ctx context.Context, messages [][]string) (string, error)
	CreateRunbook(ctx context.Context, service string, alert string, msgs []schema.ThreadMessagesV2) (string, error)
	UpdateRunbook(ctx context.Context, runbook schema.IncidentRunbook, msgs []schema.ThreadMessagesV2) (string, error)
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

func (c *client) CreateRunbook(ctx context.Context, service string, alert string, msgs []schema.ThreadMessagesV2) (string, error) {
	if c == nil {
		return "", nil
	}

	prompt := `Create a concise runbook based on the provided incident messages from past triaging. Structure as follows:

**Overview**
- Brief description of the alert and its trigger conditions

**Root Causes**
- Identified causes from past incidents (cite specific sources/messages)
- Contributing factors (with references to supporting messages)

**Resolution Steps**
- Specific troubleshooting steps taken (include references to the original messages)
- Commands used and their outcomes (cite sources when derived from past cases)
- Successful resolution actions (indicate where similar steps have worked before)

RULES:
- Format in Slack-friendly Markdown.
- Include only information explicitly mentioned in the messages.
- Cite sources when referencing historical incidents or previous actions.
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

func (c *client) UpdateRunbook(ctx context.Context, runbook schema.IncidentRunbook, msgs []schema.ThreadMessagesV2) (string, error) {
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
