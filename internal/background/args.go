package background

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

type ReportWorkerArgs struct {
	ChannelID string `json:"channel_id"`
}

func (r ReportWorkerArgs) Kind() string {
	return "report"
}

type PostRunbookWorkerArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (r PostRunbookWorkerArgs) Kind() string {
	return "post_runbook"
}

type UpdateRunbookWorkerArgs struct {
	Service       string `json:"service"`
	Alert         string `json:"alert"`
	ForceRecreate bool   `json:"force_recreate"`
}

func (u UpdateRunbookWorkerArgs) Kind() string {
	return "update_runbook"
}
