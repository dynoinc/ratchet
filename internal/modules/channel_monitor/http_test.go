package channel_monitor

import (
	"context"
	"fmt"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/dynoinc/ratchet/internal/modules/channel_monitor/mocks"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestGetTestResults_MaintainsOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create test messages with identifiable order
	messages := []dto.SlackMessage{
		{Text: "message 1"},
		{Text: "message 2"},
		{Text: "message 3"},
		{Text: "message 4"},
		{Text: "message 5"},
	}

	// Create entry with simple template
	entry := &entry{
		PromptTemplate: template.Must(template.New("test").Parse("prompt for: {{.Message.Text}}")),
	}

	// Create mock LLM client
	mockLLM := mocks.NewMockClient(ctrl)

	// Set up expectations for each message
	for i, msg := range messages {
		expectedPrompt := fmt.Sprintf("prompt for: %s", msg.Text)
		expectedOutput := fmt.Sprintf("valid output for: %s", msg.Text)
		mockLLM.EXPECT().
			RunJSONModePrompt(gomock.Any(), expectedPrompt, gomock.Any()).
			DoAndReturn(func(ctx context.Context, prompt string, schema interface{}) (string, string, error) {
				// Add small random delay to test concurrent processing
				time.Sleep(time.Duration(10+i*5) * time.Millisecond)
				return expectedOutput, "", nil
			})
	}

	// Get results
	results := getTestResults(context.Background(), messages, entry, mockLLM)

	// Verify results length
	assert.Equal(t, len(messages), len(results), "should have same number of results as messages")

	// Verify order is maintained
	for i, result := range results {
		expectedMessage := messages[i]
		assert.Equal(t, expectedMessage.Text, result.Message.Text, "message at index %d should match", i)

		expectedPrompt := fmt.Sprintf("prompt for: %s", expectedMessage.Text)
		assert.Equal(t, expectedPrompt, result.Prompt, "prompt at index %d should match", i)

		expectedOutput := fmt.Sprintf("valid output for: %s", expectedMessage.Text)
		assert.Equal(t, expectedOutput, result.ValidatedOutput, "output at index %d should match", i)
	}
}

func TestGetTestResults_HandlesErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create test messages
	messages := []dto.SlackMessage{
		{Text: "message 1"},
	}

	// Create entry with invalid template to trigger error
	entry := &entry{
		PromptTemplate: template.Must(template.New("test").Parse("{{.InvalidField}}")),
	}

	// Create mock LLM client
	mockLLM := mocks.NewMockClient(ctrl)

	// Get results
	results := getTestResults(context.Background(), messages, entry, mockLLM)

	// Verify error is captured
	assert.Equal(t, 1, len(results), "should have one result")
	assert.Contains(t, results[0].Error, "executing prompt template", "should contain template error")
}

func TestGetTestResults_ConcurrencyLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Create many test messages
	messages := make([]dto.SlackMessage, 20)
	for i := range messages {
		messages[i] = dto.SlackMessage{Text: fmt.Sprintf("message %d", i+1)}
	}

	// Create entry with simple template
	entry := &entry{
		PromptTemplate: template.Must(template.New("test").Parse("prompt for: {{.Message.Text}}")),
	}

	// Create mock LLM client
	mockLLM := mocks.NewMockClient(ctrl)

	// Set up expectations for each message with a delay
	for _, msg := range messages {
		expectedPrompt := fmt.Sprintf("prompt for: %s", msg.Text)
		expectedOutput := fmt.Sprintf("valid output for: %s", msg.Text)
		mockLLM.EXPECT().
			RunJSONModePrompt(gomock.Any(), expectedPrompt, gomock.Any()).
			DoAndReturn(func(ctx context.Context, prompt string, schema interface{}) (string, string, error) {
				time.Sleep(100 * time.Millisecond)
				return expectedOutput, "", nil
			})
	}

	start := time.Now()

	// Get results
	results := getTestResults(context.Background(), messages, entry, mockLLM)

	duration := time.Since(start)

	// With 20 messages and 100ms delay per message:
	// - If running serially: would take ~2000ms
	// - If running fully parallel: would take ~100ms
	// - With 5 concurrent limit: should take ~400ms (4 batches of 5 messages)
	// Add some buffer for processing overhead
	assert.Greater(t, duration, 400*time.Millisecond, "should not be fully parallel")
	assert.Less(t, duration, 2000*time.Millisecond, "should not be fully serial")

	// Verify all results are present and in order
	assert.Equal(t, len(messages), len(results), "should have all results")
	for i, result := range results {
		assert.Equal(t, fmt.Sprintf("message %d", i+1), result.Message.Text, "messages should be in order")
	}
}

func TestGetTestResults_LLMErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	messages := []dto.SlackMessage{
		{Text: "message 1"},
	}

	entry := &entry{
		PromptTemplate: template.Must(template.New("test").Parse("prompt for: {{.Message.Text}}")),
	}

	mockLLM := mocks.NewMockClient(ctrl)
	llmErr := fmt.Errorf("llm failure")

	mockLLM.EXPECT().
		RunJSONModePrompt(gomock.Any(), "prompt for: message 1", gomock.Any()).
		Return("", "", llmErr)

	results := getTestResults(context.Background(), messages, entry, mockLLM)

	assert.Equal(t, 1, len(results), "should have one result")
	assert.Equal(t, llmErr.Error(), results[0].Error, "should capture llm error")
	assert.Empty(t, results[0].ValidatedOutput, "validated output should be empty")
	assert.Empty(t, results[0].InvalidOutput, "invalid output should be empty")
}
