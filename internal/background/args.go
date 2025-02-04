package background

import "github.com/riverqueue/river"

type ClassifierArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (c ClassifierArgs) Kind() string {
	return "classifier"
}

type ChannelOnboardWorkerArgs struct {
	ChannelID string `json:"channel_id"`
}

func (c ChannelOnboardWorkerArgs) Kind() string {
	return "channel_board"
}

type ReportWorkerArgs struct {
	ChannelID string `json:"channel_id"`
}

func (r ReportWorkerArgs) Kind() string {
	return "report"
}

func (r ReportWorkerArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: "report",
	}
}
