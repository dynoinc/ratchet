package llm

import (
	"context"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

type LLMConfig struct {
	OpenAIAPIKey string `envconfig:"OPENAI_API_KEY" default:"fake-key"`
	OpenAIAPIURL string `envconfig:"OPENAI_API_URL" default:"https://localhost:11434"`
}

func New(ctx context.Context, c LLMConfig) (*openai.Client, error) {
	return openai.NewClient(option.WithBaseURL(c.OpenAIAPIURL), option.WithAPIKey(c.OpenAIAPIKey)), nil
}
