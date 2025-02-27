package commands

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/stretchr/testify/require"
)

func TestFindCommand(t *testing.T) {
	// Enable debug logging
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	cfg := llm.DefaultConfig()
	llmClient, err := llm.New(context.Background(), cfg, nil)
	if err != nil {
		t.Skip("LLM client not available")
	}

	commands := New(nil, nil, llmClient)

	tests := []struct {
		message string
		want    cmd
	}{
		// Weekly report requests
		{"post report", cmdPostWeeklyReport},
		{"please post the weekly report", cmdPostWeeklyReport},
		{"can you share the weekly report", cmdPostWeeklyReport},
		{"what's the status report", cmdPostWeeklyReport},
		{"generate a weekly incident report", cmdPostWeeklyReport},

		// Usage report requests
		{"post usage report", cmdPostUsageReport},
		{"show me usage statistics", cmdPostUsageReport},
		{"what are the usage numbers", cmdPostUsageReport},
		{"display bot usage data", cmdPostUsageReport},

		// Leave channel requests
		{"leave the channel", cmdLeaveChannel},
		{"please leave this channel", cmdLeaveChannel},
		{"exit channel", cmdLeaveChannel},
		{"get out of this channel", cmdLeaveChannel},

		// Invalid/unrelated requests
		{"how are you?", cmdNone},
		{"what's the weather like?", cmdNone},
		{"tell me a joke", cmdNone},
		{"", cmdNone},
	}

	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			got, err := commands.findCommand(context.Background(), test.message)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}
