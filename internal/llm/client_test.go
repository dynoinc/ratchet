package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyService(t *testing.T) {
	llmClient, err := New(t.Context(), Config{
		URL:   "http://localhost:11434/v1/",
		Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	services := []string{
		"service_a",
		"service_b",
		"target_service",
	}

	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "matches exact service",
			message: "message about target_service",
			want:    "target_service",
		},
		{
			name:    "matches service with surrounding text",
			message: "I'm having an issue with service_a when trying to deploy",
			want:    "service_a",
		},
		{
			name:    "returns empty for no match",
			message: "something completely unrelated to any service",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := llmClient.ClassifyService(
				t.Context(),
				tt.message,
				services,
			)
			require.NoError(t, err)
			require.Equal(t, tt.want, service)
		})
	}
}
