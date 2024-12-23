package report_worker

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type reportWorker struct {
	river.WorkerDefaults[background.WeeklyReportJobArgs]

	slack      *slack.Client
	db         *pgxpool.Pool
	devChannel string
}

func New(slackClient *slack.Client, db *pgxpool.Pool, devChannel string) (*reportWorker, error) {
	return &reportWorker{
		slack:      slackClient,
		db:         db,
		devChannel: devChannel,
	}, nil
}

func (w *reportWorker) Work(ctx context.Context, job *river.Job[background.WeeklyReportJobArgs]) error {
	// Calculate the time range for the report
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -7) // Last 7 days

	// Get incident statistics from database
	incidentStats, err := schema.New(w.db).GetIncidentStatsByPeriod(ctx, schema.GetIncidentStatsByPeriodParams{
		ChannelID: job.Args.ChannelID,
		StartTimestamp: pgtype.Timestamptz{
			Time:  startDate,
			Valid: true,
		},
		StartTimestamp_2: pgtype.Timestamptz{
			Time:  endDate,
			Valid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to get incident stats: %w", err)
	}

	// Get top alerts from database
	topAlerts, err := schema.New(w.db).GetTopAlerts(ctx, schema.GetTopAlertsParams{
		ChannelID: job.Args.ChannelID,
		StartTimestamp: pgtype.Timestamptz{
			Time:  startDate,
			Valid: true,
		},
		StartTimestamp_2: pgtype.Timestamptz{
			Time:  endDate,
			Valid: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to get top alerts: %w", err)
	}

	// Get channel info
	channel, err := schema.New(w.db).GetChannel(ctx, job.Args.ChannelID)
	if err != nil {
		return fmt.Errorf("failed to get channel info: %w", err)
	}
	channelName := cmp.Or(channel.Attrs.Name, job.Args.ChannelID)

	// Generate the report using the database data
	reportData, err := report.GenerateReportData(channelName, startDate, endDate, incidentStats, topAlerts)
	if err != nil {
		return fmt.Errorf("failed to generate report data: %w", err)
	}

	// Format for Slack
	slackReport := report.SlackFormatter{}.Format(reportData)

	// Post the report to Slack (to dev channel if specified)
	if _, _, err := w.slack.PostMessage(
		cmp.Or(w.devChannel, job.Args.ChannelID),
		slack.MsgOptionBlocks(slackReport...),
	); err != nil {
		return fmt.Errorf("failed to post report to channel %s: %w", job.Args.ChannelID, err)
	}

	return nil
}
