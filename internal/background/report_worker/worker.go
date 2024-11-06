package report_worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Worker struct {
	river.WorkerDefaults[background.WeeklyReportJobArgs]
	db        *pgxpool.Pool
	slack     *slack.Client
	generator *report.Generator
}

func New(db *pgxpool.Pool, slackClient *slack.Client) (*Worker, error) {
	generator, err := report.NewGenerator()
	if err != nil {
		return nil, err
	}

	return &Worker{
		db:        db,
		slack:     slackClient,
		generator: generator,
	}, nil
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.WeeklyReportJobArgs]) error {
	// 1. Get all channels from database
	channels, err := schema.New(w.db).GetSlackChannels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get channels: %w", err)
	}

	// 2. Generate and post report for each channel
	for _, channel := range channels {
		report, err := w.generator.GenerateReport(channel.ChannelID, time.Now())
		if err != nil {
			return fmt.Errorf("failed to generate report for channel %s: %w", channel.ChannelID, err)
		}

		// TODO: Create canvas and post to channel instead of message

		// Create a message attachment for better formatting
		attachment := slack.Attachment{
			Color:      "#36a64f", // Green color for the sidebar
			MarkdownIn: []string{"text"},
			Text:       report,
			Footer:     "🤖 Generated by Ratchet Bot",
			Ts:         json.Number(fmt.Sprintf("%d", time.Now().Unix())),
		}

		// Post the message to the channel
		_, _, err = w.slack.PostMessage(
			channel.ChannelID,
			slack.MsgOptionText("📊 Weekly Operations Report", false),
			slack.MsgOptionAttachments(attachment),
		)
		if err != nil {
			return fmt.Errorf("failed to post report to channel %s: %w", channel.ChannelID, err)
		}
	}

	return nil
}
