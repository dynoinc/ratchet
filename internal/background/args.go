package background

import "github.com/dynoinc/ratchet/internal/docs"

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
	ThreadTS  string `json:"thread_ts"` // non-empty if this is a thread message
}

func (m ModulesWorkerArgs) Kind() string {
	return "modules"
}

type DocumentationRefreshArgs struct {
	Source docs.Source `json:"source"`
}

func (d DocumentationRefreshArgs) Kind() string {
	return "documentation_refresh"
}
