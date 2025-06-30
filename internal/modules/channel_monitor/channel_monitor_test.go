package channel_monitor

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/dynoinc/ratchet/internal/llm/mocks"
	slackmocks "github.com/dynoinc/ratchet/internal/slack_integration/mocks"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

var testConfig = `
test-slug-1:
  channel_id: test-channel-id
  prompt: test-prompt {{.Message.Text}}
  executable: echo
  executable_args:
    - "arg1"
    - "arg2"
  result_schema:
    type: "object"
    properties:
      team:
        type: string
      reason:
        type: string
test-slug-2:
  channel_id: "test-channel-id"
  executable: echo
  prompt: "test-prompt {{.Message.Text}}"
`

func TestParseConfigFullFeature(t *testing.T) {
	c, err := parseConfig([]byte(testConfig))
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Len(t, c, 2)
	require.NotNil(t, c["test-slug-1"])
	require.Equal(t, "test-channel-id", c["test-slug-1"].ChannelID)
	require.Equal(t, "echo", c["test-slug-1"].Executable)
	require.Len(t, c["test-slug-1"].ExecutableArgs, 2)
	require.Equal(t, "arg1", c["test-slug-1"].ExecutableArgs[0])
	require.Equal(t, "arg2", c["test-slug-1"].ExecutableArgs[1])
	require.Equal(t, "test-prompt {{.Message.Text}}", c["test-slug-1"].Prompt)
	require.NotNil(t, c["test-slug-1"].PromptTemplate)
	buffer := bytes.Buffer{}
	err = c["test-slug-1"].PromptTemplate.Execute(&buffer, promptData{Message: dto.SlackMessage{Text: "test"}})
	require.NoError(t, err)
	require.Equal(t, "test-prompt test", buffer.String())
	require.NotNil(t, c["test-slug-1"].ResultSchema)
	keyErr, err := c["test-slug-1"].ResultSchema.ValidateBytes(t.Context(), []byte(`{"team":"test","reason":"test"}`))
	require.NoError(t, err)
	require.Empty(t, keyErr)
	keyErr, err = c["test-slug-1"].ResultSchema.ValidateBytes(t.Context(), []byte(`{"team":123,"reason":"test"}`))
	require.NoError(t, err)
	require.NotEmpty(t, keyErr)
}

func TestParseConfigRequirements(t *testing.T) {
	// Empty is okay
	c, err := parseConfig([]byte(``))
	require.NoError(t, err)
	require.NotNil(t, c)
	require.Len(t, c, 0)

	// Missing channel_id
	c, err = parseConfig([]byte(`
test-slug-1:
  prompt: test-prompt {{.Message.Text}}
  executable: echo
  result_schema:
    type: "object"
    properties:
      team:
        type: string
      reason:
        type: string
`))
	require.Error(t, err)
	require.Nil(t, c)

	// Missing prompt
	c, err = parseConfig([]byte(`
test-slug-1:
  channel_id: test-channel-id
  executable: echo
  result_schema:
    type: "object"
    properties:
      team:
        type: string
      reason:
        type: string`))
	require.Error(t, err)
	require.Nil(t, c)

	// Missing executable
	c, err = parseConfig([]byte(`
test-slug-1:
  channel_id: test-channel-id
  prompt: test-prompt {{.Message.Text}}
  result_schema:
    type: "object"
    properties:
      team:
        type: string
      reason:
        type: string`))
	require.Error(t, err)
	require.Nil(t, c)

	// Executable is not real file
	c, err = parseConfig([]byte(`
test-slug-1:
  channel_id: test-channel-id
  prompt: test-prompt {{.Message.Text}}
  executable: 5C70558C-4A82-44E3-AB03-6E81278CB777
  result_schema:
    type: "object"
    properties:
      team:
        type: string
      reason:
        type: string`))
	require.Error(t, err)
	require.Nil(t, c)
}

func TestHandleMessage(t *testing.T) {
	mockCtl := gomock.NewController(t)
	defer mockCtl.Finish()
	ctx := t.Context()

	mockLLM := mocks.NewMockClient(mockCtl)
	mockSlack := slackmocks.NewMockIntegration(mockCtl)

	e := entry{
		ChannelID:      "C123",
		Prompt:         "Hello {{.Message.Text}}",
		Executable:     "echo",
		ExecutableArgs: []string{`{"direct_messages": [{"email": "user@example.com", "text": "Hello world"}], "channel_messages": [{"channel_id": "C321", "text": "Demo message"}]}`},
	}

	tmpl, err := template.New("test").Parse(e.Prompt)
	require.NoError(t, err)
	e.PromptTemplate = tmpl

	cm := &channelMonitor{
		llmClient:        mockLLM,
		slackIntegration: mockSlack,
		cfg: map[string]*entry{
			"test_slug": &e,
		},
	}

	msg := dto.MessageAttrs{
		Message: dto.SlackMessage{Text: "world"},
	}

	mockLLM.EXPECT().RunJSONModePrompt(ctx, "Hello world", e.ResultSchema).Return("{\"response\": \"ok\"}", "", nil).Times(1)
	mockSlack.EXPECT().GetUserIDByEmail(ctx, "user@example.com").Return("U123", nil).Times(1)
	mockSlack.EXPECT().PostMessage(ctx, "U123", gomock.Any()).Return(nil).Times(1)
	mockSlack.EXPECT().PostMessage(ctx, "C321", gomock.Any()).Return(nil).Times(1)

	err = cm.OnMessage(ctx, "C123", "12345", msg)
	assert.NoError(t, err)
}

func TestRunExecutable(t *testing.T) {
	tests := []struct {
		name           string
		executable     string
		executableArgs []string
		expectedOutput string
		expectedError  bool
		timeout        bool
	}{
		{
			name:           "successful execution with echo",
			executable:     "echo",
			executableArgs: []string{"hello world"},
			expectedOutput: "hello world\n",
			expectedError:  false,
		},
		{
			name:          "command not found",
			executable:    "nonexistent-command-12345",
			expectedError: true,
		},
		{
			name:           "command that fails",
			executable:     "sh",
			executableArgs: []string{"-c", "echo 'to stdout' && echo 'to stderr' >&2 && exit 1"},
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			cm := &channelMonitor{}

			entry := &entry{
				ChannelID:      "C123",
				Executable:     tt.executable,
				ExecutableArgs: tt.executableArgs,
			}

			msg := dto.MessageAttrs{
				Message: dto.SlackMessage{
					Text: "test message",
					User: "U123",
				},
			}

			output, err := cm.runExecutable(ctx, "test-slug", entry, "1234567890.123456", `{"test": "data"}`, msg)

			if tt.expectedError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			assert.Equal(t, tt.expectedOutput, output)
		})
	}
}

func TestRunExecutableTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	ctx := t.Context()
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	cm := &channelMonitor{}

	entry := &entry{
		ChannelID:      "C123",
		Executable:     "sleep",
		ExecutableArgs: []string{"2"},
	}

	msg := dto.MessageAttrs{
		Message: dto.SlackMessage{Text: "test"},
	}

	_, err := cm.runExecutable(ctx, "test-slug", entry, "123", `{}`, msg)

	assert.Error(t, err)
	// The timeout can manifest as either "context deadline exceeded" or "signal: killed"
	// depending on how the context cancellation is handled
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "context deadline exceeded") ||
			strings.Contains(errMsg, "signal: killed") ||
			strings.Contains(errMsg, "killed"),
		"Expected timeout-related error, got: %s", errMsg)
}
