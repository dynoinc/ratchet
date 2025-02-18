package commands

import (
	"context"
	"testing"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/stretchr/testify/require"
)

func TestFindCommand(t *testing.T) {
	llmClient, err := llm.New(context.Background(), llm.DefaultConfig())
	if err != nil {
		t.Skip("ollama not running")
	}

	commands := New(nil, nil, llmClient)

	tests := []struct {
		name    string
		message string
		want    cmd
	}{
		{
			name:    "post weekly report to slack channel",
			message: "post report",
			want:    cmdPostReport,
		},
		{
			name:    "how are you?",
			message: "how are you?",
			want:    cmdNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, score, err := commands.findCommand(t.Context(), test.message)
			require.NoError(t, err)
			t.Logf("score: %v", score)
			require.Equal(t, test.want, got)
		})
	}
}
