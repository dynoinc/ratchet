package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateEmbedding(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig())
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	embedding, err := llmClient.GenerateEmbedding(t.Context(), "classification", "Hello, world!")
	require.NoError(t, err)
	require.Equal(t, 768, len(embedding))
}
