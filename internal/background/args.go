package background

import "github.com/riverqueue/river/rivertype"

func init() {
	// Register job types with River
	rivertype.RegisterJob[ClassifierWorkerArgs]("classifier")
	rivertype.RegisterJob[BackfillThreadWorkerArgs]("backfill_thread")
	rivertype.RegisterJob[ChannelOnboardWorkerArgs]("channel_onboard")
	rivertype.RegisterJob[ModulesWorkerArgs]("modules")
	rivertype.RegisterJob[LLMUsageCleanupWorkerArgs]("llm_usage_cleanup")
}

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
