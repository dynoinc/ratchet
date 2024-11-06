package report_worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/report"
)

type Worker struct {
	river.WorkerDefaults[background.WeeklyReportJobArgs]
	slack     *slack.Client
	generator *report.Generator
}

func New(slackClient *slack.Client) (*Worker, error) {
	generator, err := report.NewGenerator()
	if err != nil {
		return nil, err
	}

	return &Worker{
		slack:     slackClient,
		generator: generator,
	}, nil
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.WeeklyReportJobArgs]) error {

	report, err := w.generator.GenerateReport(job.Args.ChannelID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to generate report for channel %s: %w", job.Args.ChannelID, err)
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
		job.Args.ChannelID,
		slack.MsgOptionText("📊 Weekly Operations Report", false),
		slack.MsgOptionAttachments(attachment),
	)
	if err != nil {
		return fmt.Errorf("failed to post report to channel %s: %w", job.Args.ChannelID, err)
	}

	return nil
}
