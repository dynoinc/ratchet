package report_worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type ReportWorker struct {
	river.WorkerDefaults[background.WeeklyReportJobArgs]
	slack     *slack.Client
	generator *report.Generator
	db        *pgxpool.Pool
}

func New(slackClient *slack.Client, db *pgxpool.Pool) (*ReportWorker, error) {
	generator, err := report.NewGenerator()
	if err != nil {
		return nil, err
	}

	return &ReportWorker{
		slack:     slackClient,
		generator: generator,
		db:        db,
	}, nil
}

func (w *ReportWorker) Work(ctx context.Context, job *river.Job[background.WeeklyReportJobArgs]) error {
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

	// If there are no incidents or alerts, don't generate a report
	if len(incidentStats) == 0 && len(topAlerts) == 0 {
		return nil
	}

	// Get channel info
	channel, err := schema.New(w.db).GetChannel(ctx, job.Args.ChannelID)
	if err != nil {
		return fmt.Errorf("failed to get channel info: %w", err)
	}

	// Get channel name from attrs, fallback to channel ID if not found
	channelName := job.Args.ChannelID
	if len(channel.Attrs) > 0 {
		var attrs dto.ChannelAttrs
		if err := json.Unmarshal(channel.Attrs, &attrs); err == nil && attrs.Name != "" {
			channelName = attrs.Name
		}
	}

	// Generate the report using the database data
	reportData, err := w.generator.GenerateReportData(channelName, startDate, incidentStats, topAlerts)
	if err != nil {
		return fmt.Errorf("failed to generate report data: %w", err)
	}

	// Format for Slack
	slackReport := w.generator.FormatForSlack(reportData)

	// Create Slack blocks using slackReport
	blocks := createSlackBlocks(slackReport)

	// Store the structured report data
	reportDataJSON, err := json.Marshal(reportData)
	if err != nil {
		return fmt.Errorf("failed to marshal report data: %w", err)
	}

	// Store in database
	_, err = schema.New(w.db).CreateReport(ctx, schema.CreateReportParams{
		ChannelID:         job.Args.ChannelID,
		ReportPeriodStart: pgtype.Timestamptz{Time: reportData.WeekRange.Start, Valid: true},
		ReportPeriodEnd:   pgtype.Timestamptz{Time: reportData.WeekRange.End, Valid: true},
		ReportData:        reportDataJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to store report: %w", err)
	}

	// Post the report to Slack
	_, _, err = w.slack.PostMessage(
		job.Args.ChannelID,
		slack.MsgOptionBlocks(blocks...),
	)
	if err != nil {
		return fmt.Errorf("failed to post report to channel %s: %w", job.Args.ChannelID, err)
	}

	return nil
}

func createSlackBlocks(report *report.SlackReport) []slack.Block {
	return []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text", "📊 Weekly Operations Report", true, false),
		),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("Channel: #%s\n📅 Week: %s",
					report.ChannelName,
					report.WeekRange,
				),
				false, false,
			),
			nil, nil,
		),
		// Incidents section header
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text", "📈 Incidents by Severity", true, false),
		),
		// Incidents table
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "```\n"+report.IncidentsByType+"```", false, false),
			nil, nil,
		),
		// Average mitigation time
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("⏱️ Average Mitigation Time: %s", report.AvgMitigationTime),
				false, false,
			),
			nil, nil,
		),
		// Top alerts section header
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text", "🔥 Top Alerts", true, false),
		),
		// Top alerts table
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "```\n"+report.TopAlerts+"```", false, false),
			nil, nil,
		),
		// Footer
		slack.NewContextBlock(
			"footer",
			slack.NewTextBlockObject(
				"mrkdwn",
				fmt.Sprintf("🤖 Generated by Ratchet Bot | %s", time.Now().Format(time.RFC1123)),
				false, false,
			),
		),
	}
}
