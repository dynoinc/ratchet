package background

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ClassifierArgs struct {
	ChannelID  string `json:"channel_id"`
	SlackTS    string `json:"slack_ts"`
	IsBackfill bool   `json:"is_backfill"`
}

func (c ClassifierArgs) Kind() string {
	return "classifier"
}

type ChannelOnboardWorkerArgs struct {
	ChannelID string `json:"channel_id"`
	LastNMsgs int    `json:"last_n_msgs"`
}

func (c ChannelOnboardWorkerArgs) Kind() string {
	return "channel_board"
}

type BackfillThreadWorkerArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (b BackfillThreadWorkerArgs) Kind() string {
	return "backfill_thread"
}

type ModulesWorkerArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (m ModulesWorkerArgs) Kind() string {
	return "modules"
}

type LLMUsageCleanupWorkerArgs struct {
	RetentionDays int `json:"retention_days"`
}

func (l LLMUsageCleanupWorkerArgs) Kind() string {
	return "llm_usage_cleanup"
}

// LLMUsageRecordWorkerArgs contains arguments for the LLM usage record worker
// This should match the schema.LlmUsageV1 structure
type LLMUsageRecordWorkerArgs struct {
	ID               uuid.UUID       `json:"id,omitempty"`
	CreatedAt        time.Time       `json:"created_at,omitempty"`
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

func (l LLMUsageRecordWorkerArgs) Kind() string {
	return "llm_usage_record"
}
