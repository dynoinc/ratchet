package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// LLMUsage represents an entry in the llm_usage_v1 table
type LLMUsage struct {
	ID               uuid.UUID       `json:"id"`
	CreatedAt        time.Time       `json:"created_at"`
	Model            string          `json:"model"`
	OperationType    string          `json:"operation_type"`
	PromptText       string          `json:"prompt_text"`
	CompletionText   string          `json:"completion_text,omitempty"`
	PromptTokens     int             `json:"prompt_tokens,omitempty"`
	CompletionTokens int             `json:"completion_tokens,omitempty"`
	TotalTokens      int             `json:"total_tokens,omitempty"`
	LatencyMs        int             `json:"latency_ms,omitempty"`
	Status           string          `json:"status"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

// Valid operation types for the LLMUsage.OperationType field
const (
	OpTypeCompletion     = "completion"
	OpTypeEmbedding      = "embedding"
	OpTypeClassification = "classification"
	OpTypeRunbook        = "runbook"
	OpTypeJSONPrompt     = "json_prompt"
)

// Valid status types for the LLMUsage.Status field
const (
	StatusSuccess = "success"
	StatusError   = "error"
)
