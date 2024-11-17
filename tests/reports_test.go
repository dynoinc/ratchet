package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func TestReports(t *testing.T) {
	bot := SetupBot(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("can store and retrieve reports", func(t *testing.T) {
		// First create a channel
		channelID := "test-channel"
		channelName := "test-channel-name"
		_, err := bot.AddChannel(ctx, channelID, channelName)
		require.NoError(t, err)

		// Create test report data
		now := time.Now().UTC()
		reportData := report.ReportData{
			ChannelName: channelName,
			WeekRange: report.DateRange{
				Start: now.AddDate(0, 0, -7),
				End:   now,
			},
			Incidents: []report.Incident{
				{
					Severity:    "P0",
					Count:       1,
					TotalTime:   time.Hour,
					AverageTime: time.Hour,
				},
			},
			TopAlerts: []report.Alert{
				{
					Name:        "Test Alert",
					Count:       1,
					LastSeen:    now,
					AverageTime: time.Hour,
				},
			},
		}

		reportDataJSON, err := json.Marshal(reportData)
		require.NoError(t, err)

		// Store the report
		queries := schema.New(bot.DB)
		storedReport, err := queries.CreateReport(ctx, schema.CreateReportParams{
			ChannelID: channelID,
			ReportPeriodStart: pgtype.Timestamptz{
				Time:  reportData.WeekRange.Start,
				Valid: true,
			},
			ReportPeriodEnd: pgtype.Timestamptz{
				Time:  reportData.WeekRange.End,
				Valid: true,
			},
			ReportData: reportDataJSON,
		})
		require.NoError(t, err)
		require.NotNil(t, storedReport)

		// Test list view retrieval
		reportsList, err := queries.GetChannelReports(ctx, schema.GetChannelReportsParams{
			ChannelID: channelID,
			Limit:     10,
		})
		require.NoError(t, err)
		require.Len(t, reportsList, 1)
		require.Equal(t, channelID, reportsList[0].ChannelID)
		require.Equal(t, channelName, reportsList[0].ChannelName)
		require.Equal(t, reportData.WeekRange.Start.Unix(), reportsList[0].ReportPeriodStart.Time.Unix())
		require.Equal(t, reportData.WeekRange.End.Unix(), reportsList[0].ReportPeriodEnd.Time.Unix())

		// Test detailed view retrieval
		detailedReport, err := queries.GetReport(ctx, reportsList[0].ID)
		require.NoError(t, err)
		require.Equal(t, channelName, detailedReport.ChannelName)
		var retrievedData report.ReportData
		err = json.Unmarshal(detailedReport.ReportData, &retrievedData)
		require.NoError(t, err)
		require.Equal(t, reportData.ChannelName, retrievedData.ChannelName)
		require.Equal(t, reportData.WeekRange.Start.Unix(), retrievedData.WeekRange.Start.Unix())
		require.Equal(t, reportData.WeekRange.End.Unix(), retrievedData.WeekRange.End.Unix())
		require.Equal(t, len(reportData.Incidents), len(retrievedData.Incidents))
		require.Equal(t, len(reportData.TopAlerts), len(retrievedData.TopAlerts))
	})

	t.Run("enforces unique constraint on time period", func(t *testing.T) {
		channelID := "test-channel-2"
		channelName := "test-channel-2-name"
		_, err := bot.AddChannel(ctx, channelID, channelName)
		require.NoError(t, err)

		now := time.Now().UTC()
		reportData := report.ReportData{
			ChannelName: channelName,
			WeekRange: report.DateRange{
				Start: now.AddDate(0, 0, -7),
				End:   now,
			},
		}

		reportDataJSON, err := json.Marshal(reportData)
		require.NoError(t, err)

		queries := schema.New(bot.DB)

		// Store first report
		_, err = queries.CreateReport(ctx, schema.CreateReportParams{
			ChannelID: channelID,
			ReportPeriodStart: pgtype.Timestamptz{
				Time:  reportData.WeekRange.Start,
				Valid: true,
			},
			ReportPeriodEnd: pgtype.Timestamptz{
				Time:  reportData.WeekRange.End,
				Valid: true,
			},
			ReportData: reportDataJSON,
		})
		require.NoError(t, err)

		// Attempt to store second report with same period
		_, err = queries.CreateReport(ctx, schema.CreateReportParams{
			ChannelID: channelID,
			ReportPeriodStart: pgtype.Timestamptz{
				Time:  reportData.WeekRange.Start,
				Valid: true,
			},
			ReportPeriodEnd: pgtype.Timestamptz{
				Time:  reportData.WeekRange.End,
				Valid: true,
			},
			ReportData: reportDataJSON,
		})
		require.Error(t, err) // Should fail due to unique constraint
	})

	t.Run("cascade deletes reports when channel is deleted", func(t *testing.T) {
		channelID := "test-channel-3"
		channelName := "test-channel-3-name"
		_, err := bot.AddChannel(ctx, channelID, channelName)
		require.NoError(t, err)

		now := time.Now().UTC()
		reportData := report.ReportData{
			ChannelName: channelName,
			WeekRange: report.DateRange{
				Start: now.AddDate(0, 0, -7),
				End:   now,
			},
		}

		reportDataJSON, err := json.Marshal(reportData)
		require.NoError(t, err)

		queries := schema.New(bot.DB)

		// Store a report
		_, err = queries.CreateReport(ctx, schema.CreateReportParams{
			ChannelID: channelID,
			ReportPeriodStart: pgtype.Timestamptz{
				Time:  reportData.WeekRange.Start,
				Valid: true,
			},
			ReportPeriodEnd: pgtype.Timestamptz{
				Time:  reportData.WeekRange.End,
				Valid: true,
			},
			ReportData: reportDataJSON,
		})
		require.NoError(t, err)

		// Delete the channel
		err = queries.RemoveChannel(ctx, channelID)
		require.NoError(t, err)

		// Verify reports were deleted
		reports, err := queries.GetChannelReports(ctx, schema.GetChannelReportsParams{
			ChannelID: channelID,
			Limit:     10,
		})
		require.NoError(t, err)
		require.Len(t, reports, 0)
	})
}
