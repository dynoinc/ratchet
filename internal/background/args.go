package background

type ClassifierArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (c ClassifierArgs) Kind() string {
	return "classifier"
}

type MessagesIngestionWorkerArgs struct {
	ChannelID        string `json:"channel_id"`
	SlackTSWatermark string `json:"slack_ts_watermark"`
}

func (m MessagesIngestionWorkerArgs) Kind() string {
	return "ingest_messages"
}

type WeeklyReportJobArgs struct {
	ChannelID string `json:"channel_id"`
}

func (w WeeklyReportJobArgs) Kind() string {
	return "weekly_report"
}

type ChannelInfoWorkerArgs struct {
	ChannelID string `json:"channel_id"`
}

func (c ChannelInfoWorkerArgs) Kind() string {
	return "channel_info"
}
