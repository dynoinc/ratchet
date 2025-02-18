package commands

import (
	"context"
	"testing"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/stretchr/testify/require"
)

func TestFindCommand(t *testing.T) {
	cfg := llm.DefaultConfig()
	llmClient, err := llm.New(context.Background(), cfg)
	if err != nil {
		t.Skip("ollama not running")
	}

	commands := New(nil, nil, llmClient)

	tests := []struct {
		message string
		want    cmd
	}{
		// Valid report requests
		{"post report", cmdPostReport},
		{"please post the weekly report", cmdPostReport},
		{"can you share the weekly report", cmdPostReport},
		{"what's the status report", cmdPostReport},
		{"hey show me the report", cmdPostReport},
		{"need the weekly report asap", cmdPostReport},
		{"could you please post the report", cmdPostReport},
		{"give me an update on the report", cmdPostReport},
		{"generate a report", cmdPostReport},
		{"create a new report", cmdPostReport},
		{"publish the weekly report", cmdPostReport},
		{"send me the report", cmdPostReport},

		// Invalid/unrelated requests
		{"how are you?", cmdNone},
		{"let's have a conversation", cmdNone},
		{"what can you do?", cmdNone},
		{"tell me a joke", cmdNone},
		{"", cmdNone},
	}

	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			got, score, err := commands.findCommand(t.Context(), test.message)
			require.NoError(t, err)
			t.Logf("score: %v", score)
			require.Equal(t, test.want, got)
		})
	}
}
