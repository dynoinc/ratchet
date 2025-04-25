package background

import "github.com/dynoinc/ratchet/internal/docs"

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
	ChannelID  string `json:"channel_id"`
	SlackTS    string `json:"slack_ts"`
	ParentTS   string `json:"parent_ts"`
	IsBackfill bool   `json:"is_backfill"`
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
