package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateEmbedding(t *testing.T) {
	llmClient, err := New(t.Context(), Config{
		URL:            "http://localhost:11434/v1/",
		Model:          "qwen2.5:7b",
		EmbeddingModel: "all-minilm",
	})
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	embedding, err := llmClient.GenerateEmbedding(t.Context(), "Hello, world!")
	require.NoError(t, err)
	require.Equal(t, 384, len(embedding))
}
