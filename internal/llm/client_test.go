package llm

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestGenerateEmbedding(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	embedding, err := llmClient.GenerateEmbedding(t.Context(), "classification", "Hello, world!")
	require.NoError(t, err)
	require.Equal(t, 768, len(embedding))
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

	resp, respMsg, err := llmClient.RunJSONModePrompt(t.Context(), `Return the json message {"hello": "world"}`, schema)
	require.NoError(t, err)
	require.Empty(t, respMsg)
	space := regexp.MustCompile(`\s+`)
	require.Equal(t, `{"hello":"world"}`, space.ReplaceAllString(resp, ""))

	resp, respMsg, err = llmClient.RunJSONModePrompt(t.Context(), `Return the json message {"hello": 1}`, schema)
	require.Error(t, err)
	require.Empty(t, resp)
	require.Equal(t, `{"hello":1}`, space.ReplaceAllString(respMsg, ""))
}

func TestModelsEndpointHandling(t *testing.T) {
	// Test extractInputFromRequest for models endpoint
	path := "/v1/models/gpt-4.1"
	body := []byte{} // Models endpoint typically has no body

	model, input := extractInputFromRequest(path, body)

	require.Equal(t, "gpt-4.1", model)
	require.Empty(t, input.Messages)
	require.Empty(t, input.Text)
	require.Empty(t, input.Parameters)

	// Test extractOutputFromResponse for models endpoint
	modelResponse := `{
		"id": "gpt-4.1",
		"object": "model",
		"created": 1686935002,
		"owned_by": "openai"
	}`

	output := extractOutputFromResponse(200, []byte(modelResponse))

	// Debug output
	t.Logf("Output: %+v", output)
	t.Logf("Content: %q", output.Content)
	t.Logf("Error: %q", output.Error)

	require.Empty(t, output.Error)
	require.Contains(t, output.Content, "Model: gpt-4.1")
	require.Contains(t, output.Content, "Created: 1686935002")
	require.Contains(t, output.Content, "OwnedBy: openai")
	require.Nil(t, output.Usage)
	require.Nil(t, output.Embedding)
}
