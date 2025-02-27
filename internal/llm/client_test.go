package llm

import (
	"context"
	"encoding/json"
	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/mock"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// MockDB for testing
type MockDB struct {
	mock.Mock
}

func (m *MockDB) RecordLLMUsage(ctx context.Context, params interface{}) error {
	args := m.Called(ctx, params)
	return args.Error(0)
}

func TestGenerateEmbedding(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	embedding, err := llmClient.GenerateEmbedding(t.Context(), "classification", "Hello, world!")
	require.NoError(t, err)
	require.Equal(t, 768, len(embedding))
}

func TestRecordUsage(t *testing.T) {
	mockDB := new(MockDB)

	// Setup expectation
	mockDB.On("RecordLLMUsage", mock.Anything, mock.MatchedBy(func(params RecordLLMUsageParams) bool {
		return params.Model == "test-model" &&
			params.OperationType == "test-operation" &&
			params.Status == StatusSuccess
	})).Return(nil)

	client := &client{
		cfg: Config{Model: "test-model"},
		db:  mockDB,
	}

	// Test the RecordUsage method
	err := client.RecordUsage(context.Background(), &UsageRecord{
		Model:            "test-model",
		OperationType:    "test-operation",
		PromptText:       "test prompt",
		CompletionText:   "test completion",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		LatencyMs:        200,
		Status:           StatusSuccess,
	})

	require.NoError(t, err)
	mockDB.AssertExpectations(t)
}

func TestJSONSchemaValidator(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}
	schemaJSON := `{
		"type": "object",
		"properties": {
			"hello": {
				"type": "string"
			}
		}	
	}`
	schema := &jsonschema.Schema{}
	err = json.Unmarshal([]byte(schemaJSON), schema)
	require.NoError(t, err)

	resp, err := llmClient.RunJSONModePrompt(t.Context(), `Return the json message {"hello": "world"}`, schema)
	require.NoError(t, err)
	space := regexp.MustCompile(`\s+`)
	require.Equal(t, `{"hello":"world"}`, space.ReplaceAllString(resp, ""))

	resp, err = llmClient.RunJSONModePrompt(t.Context(), `Return the json message {"hello": 1}`, schema)
	require.Error(t, err)
	require.Empty(t, resp)
}
