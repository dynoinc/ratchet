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

		// Leave channel requests
		{"leave the channel", cmdLeaveChannel},
		{"quit the channel", cmdLeaveChannel},
		{"exit the channel", cmdLeaveChannel},
		{"leave this channel", cmdLeaveChannel},
		{"quit this channel", cmdLeaveChannel},
		{"exit this channel", cmdLeaveChannel},
		{"leave the channel please", cmdLeaveChannel},
		{"quit the channel please", cmdLeaveChannel},
		{"exit the channel please", cmdLeaveChannel},
		{"leave this channel please", cmdLeaveChannel},
		{"quit this channel please", cmdLeaveChannel},
		{"exit this channel please", cmdLeaveChannel},
		{"leave the channel again", cmdLeaveChannel},
		{"quit the channel again", cmdLeaveChannel},
		{"exit the channel again", cmdLeaveChannel},
		{"leave this channel again", cmdLeaveChannel},
		{"quit this channel again", cmdLeaveChannel},
		{"exit this channel again", cmdLeaveChannel},
		{"leave", cmdLeaveChannel},
		{"quit", cmdLeaveChannel},
		{"exit", cmdLeaveChannel},

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
