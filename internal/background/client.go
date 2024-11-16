package background

import (
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type ClassifierArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (c ClassifierArgs) Kind() string {
	return "classifier"
}

type MessagesIngestionWorkerArgs struct {
	ChannelID     string `json:"channel_id"`
	OldestSlackTS string `json:"oldest_slack_ts"`
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

func New(db *pgxpool.Pool, workers *river.Workers) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {
				MaxWorkers: 10,
			},
		},
		Workers: workers,
	})
}
