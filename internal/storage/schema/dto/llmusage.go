package dto

// LLMInput represents the input sent to an LLM
type LLMInput struct {
	// Messages is a list of messages for chat completions
	Messages []LLMMessage `json:"messages,omitempty"`
	// Text is the raw text input for embeddings or single-prompt requests
	Text string `json:"text,omitempty"`
	// Parameters contains any model-specific parameters
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// LLMMessage represents a message in a chat completion
type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LLMOutput represents the output received from an LLM
type LLMOutput struct {
	// Content is the generated text response
	Content string `json:"content,omitempty"`
	// Embedding is the vector representation for embedding requests
	Embedding []float32 `json:"embedding,omitempty"`
	// Usage contains token usage statistics
	Usage *LLMUsageStats `json:"usage,omitempty"`
	// Error contains any error information
	Error string `json:"error,omitempty"`
}

// LLMUsageStats tracks token usage for the request
type LLMUsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
