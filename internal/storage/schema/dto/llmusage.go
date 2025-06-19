package dto

type LLMInput struct {
	Messages   []LLMMessage           `json:"messages,omitempty"`
	Text       string                 `json:"text,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMOutput struct {
	Content   string         `json:"content,omitempty"`
	Embedding []float32      `json:"embedding,omitempty"`
	Usage     *LLMUsageStats `json:"usage,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type LLMUsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
