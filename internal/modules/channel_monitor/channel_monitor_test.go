//go:generate go tool mockgen -destination=mocks/mock_llm_client.go -package=mocks -source=../../llm/client.go Client
//go:generate go tool mockgen -destination=mocks/mock_slack_integration.go -package=mocks -source=../../slack_integration/slack.go Integration
package channel_monitor

import (
	"bytes"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/dynoinc/ratchet/internal/modules/channel_monitor/mocks"
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
	err = c["test-slug-1"].PromptTemplate.Execute(&buffer, PromptData{Message: dto.SlackMessage{Text: "test"}})
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
	mockSlack := mocks.NewMockIntegration(mockCtl)

	entry := Entry{
		ChannelID:      "C123",
		Prompt:         "Hello {{.Message.Text}}",
		Executable:     "echo",
		ExecutableArgs: []string{`{"direct_messages": [{"email": "user@example.com", "text": "Hello world"}], "channel_messages": [{"channel_id": "C321", "text": "Demo message"}]}`},
	}

	tmpl, err := template.New("test").Parse(entry.Prompt)
	require.NoError(t, err)
	entry.PromptTemplate = tmpl

	cm := &channelMonitor{
		llmClient:        mockLLM,
		slackIntegration: mockSlack,
		cfg: map[string]*Entry{
			"test_slug": &entry,
		},
	}

	msg := dto.MessageAttrs{
		Message: dto.SlackMessage{Text: "world"},
	}

	mockLLM.EXPECT().RunJSONModePrompt(ctx, "Hello world", entry.ResultSchema).Return("{\"response\": \"ok\"}", "", nil).Times(1)
	mockSlack.EXPECT().GetUserIDByEmail(ctx, "user@example.com").Return("U123", nil).Times(1)
	mockSlack.EXPECT().PostMessage(ctx, "U123", gomock.Any()).Return(nil).Times(1)
	mockSlack.EXPECT().PostMessage(ctx, "C321", gomock.Any()).Return(nil).Times(1)

	err = cm.Handle(ctx, "C123", "12345", msg)
	assert.NoError(t, err)
}
