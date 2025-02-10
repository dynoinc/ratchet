package internal

import (
	"context"
	"testing"

	"github.com/dynoinc/ratchet/internal/llm"
)

func TestFindCommand(t *testing.T) {
	llmClient, err := llm.New(context.Background(), llm.Config{
		URL:            "http://localhost:11434/v1/",
		Model:          "qwen2.5:7b",
		EmbeddingModel: "all-minilm",
	})
	if err != nil {
		t.Skip("ollama not running")
	}

	commands, err := prepareCommands(t.Context(), llmClient)
	if err != nil {
		t.Fatalf("failed to prepare commands: %v", err)
	}

	tests := []struct {
		name    string
		message string
		want    cmd
	}{
		{
			name:    "post weekly report to slack channel",
			message: "<@U0700000000> post report",
			want:    cmdPostReport,
		},
		{
			name:    "how are you?",
			message: "<@U0700000000> how are you?",
			want:    cmdNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := commands.findCommand(t.Context(), test.message)
			if err != nil {
				t.Fatalf("failed to find command: %v", err)
			}
			if got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}
