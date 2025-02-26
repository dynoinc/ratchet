package storage

import (
	"context"
	"encoding/json"
	"fmt"

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
func (s *LLMUsageService) RecordLLMUsage(ctx context.Context, params RecordLLMUsageParams) error {
	// If metadata is nil, use an empty JSON object
	metadata := params.Metadata
	if metadata == nil {
		metadata = []byte("{}")
	}

	// Map our parameters to the sqlc-generated struct
	var completionText *string
	if params.CompletionText != "" {
		completionText = &params.CompletionText
	}

	var errorMessage *string
	if params.ErrorMessage != "" {
		errorMessage = &params.ErrorMessage
	}

	// Create optional values
	var promptTokens, completionTokens, totalTokens, latencyMs *int32
	if params.PromptTokens > 0 {
		promptTokens = &params.PromptTokens
	}
	if params.CompletionTokens > 0 {
		completionTokens = &params.CompletionTokens
	}
	if params.TotalTokens > 0 {
		totalTokens = &params.TotalTokens
	}
	if params.LatencyMs > 0 {
		latencyMs = &params.LatencyMs
	}

	// Use the sqlc-generated function
	_, err := s.query.RecordLLMUsage(ctx, schema.RecordLLMUsageParams{
		Model:            params.Model,
		OperationType:    params.OperationType,
		PromptText:       params.PromptText,
		CompletionText:   completionText,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		LatencyMs:        latencyMs,
		Status:           params.Status,
		ErrorMessage:     errorMessage,
		Metadata:         metadata,
	})

	if err != nil {
		return fmt.Errorf("recording LLM usage: %w", err)
	}

	return nil
}