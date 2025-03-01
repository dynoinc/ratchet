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
