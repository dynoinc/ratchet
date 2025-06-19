package providers

import (
	"context"

	"github.com/openai/openai-go"
)

type ToolManager interface {
	ListTools(ctx context.Context) ([]openai.ChatCompletionToolParam, error)
	CallTools(ctx context.Context, toolCalls []openai.ChatCompletionMessageToolCall) (map[string]string, error)
}
