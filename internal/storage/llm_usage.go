package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// LLMUsageService provides methods for storing and retrieving LLM usage data
type LLMUsageService struct {
	db *pgxpool.Pool
}

// NewLLMUsageService creates a new LLMUsageService
func NewLLMUsageService(db *pgxpool.Pool) *LLMUsageService {
	return &LLMUsageService{db: db}
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

	query := `
	INSERT INTO llm_usage_v1 (
		model,
		operation_type,
		prompt_text,
		completion_text,
		prompt_tokens,
		completion_tokens,
		total_tokens,
		latency_ms,
		status,
		error_message,
		metadata
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
	) RETURNING id`

	var id string
	err := s.db.QueryRow(ctx, query,
		params.Model,
		params.OperationType,
		params.PromptText,
		params.CompletionText,
		params.PromptTokens,
		params.CompletionTokens,
		params.TotalTokens,
		params.LatencyMs,
		params.Status,
		params.ErrorMessage,
		metadata,
	).Scan(&id)

	if err != nil {
		return fmt.Errorf("recording LLM usage: %w", err)
	}

	return nil
}