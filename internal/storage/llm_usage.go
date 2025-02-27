package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LLMUsageService provides methods for storing and retrieving LLM usage data
type LLMUsageService struct {
	db    *pgxpool.Pool
	query *schema.Queries
}

// NewLLMUsageService creates a new LLMUsageService
func NewLLMUsageService(db *pgxpool.Pool) *LLMUsageService {
	return &LLMUsageService{
		db:    db,
		query: schema.New(db),
	}
}

// RecordLLMUsageParams contains parameters for recording LLM usage
type RecordLLMUsageParams struct {
	Model            string          `json:"model"`
	OperationType    string          `json:"operation_type"`
	PromptText       string          `json:"prompt_text"`
	CompletionText   string          `json:"completion_text,omitempty"`
	PromptTokens     int32           `json:"prompt_tokens,omitempty"`
	CompletionTokens int32           `json:"completion_tokens,omitempty"`
	TotalTokens      int32           `json:"total_tokens,omitempty"`
	LatencyMs        int32           `json:"latency_ms,omitempty"`
	Status           string          `json:"status"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
}

// RecordLLMUsage records LLM usage data in the database
func (s *LLMUsageService) RecordLLMUsage(ctx context.Context, params interface{}) error {
	var llmParams RecordLLMUsageParams

	// Check if the params are from the LLM package or our own
	switch p := params.(type) {
	case RecordLLMUsageParams:
		llmParams = p
	case llm.RecordLLMUsageParams:
		// Convert from LLM package params to our params
		llmParams = RecordLLMUsageParams{
			Model:            p.Model,
			OperationType:    p.OperationType,
			PromptText:       p.PromptText,
			CompletionText:   p.CompletionText,
			PromptTokens:     p.PromptTokens,
			CompletionTokens: p.CompletionTokens,
			TotalTokens:      p.TotalTokens,
			LatencyMs:        p.LatencyMs,
			Status:           p.Status,
			ErrorMessage:     p.ErrorMessage,
			Metadata:         p.Metadata,
		}
	default:
		return fmt.Errorf("unsupported params type: %T", params)
	}

	// If metadata is nil, use an empty JSON object
	metadata := llmParams.Metadata
	if metadata == nil {
		metadata = []byte("{}")
	}

	// Map our parameters to the sqlc-generated struct
	var completionText *string
	if llmParams.CompletionText != "" {
		completionText = &llmParams.CompletionText
	}

	var errorMessage *string
	if llmParams.ErrorMessage != "" {
		errorMessage = &llmParams.ErrorMessage
	}

	// Create optional values
	var promptTokens, completionTokens, totalTokens, latencyMs *int32
	if llmParams.PromptTokens > 0 {
		promptTokens = &llmParams.PromptTokens
	}
	if llmParams.CompletionTokens > 0 {
		completionTokens = &llmParams.CompletionTokens
	}
	if llmParams.TotalTokens > 0 {
		totalTokens = &llmParams.TotalTokens
	}
	if llmParams.LatencyMs > 0 {
		latencyMs = &llmParams.LatencyMs
	}

	// Use the sqlc-generated function
	_, err := s.query.RecordLLMUsage(ctx, schema.RecordLLMUsageParams{
		Model:            llmParams.Model,
		OperationType:    llmParams.OperationType,
		PromptText:       llmParams.PromptText,
		CompletionText:   completionText,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		LatencyMs:        latencyMs,
		Status:           llmParams.Status,
		ErrorMessage:     errorMessage,
		Metadata:         metadata,
	})

	if err != nil {
		return fmt.Errorf("recording LLM usage: %w", err)
	}

	return nil
}
