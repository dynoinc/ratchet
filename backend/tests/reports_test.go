package tests

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestReports(t *testing.T) {
	bot := SetupBot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("can store and retrieve reports", func(t *testing.T) {
		// First create a channel
		channelID := "test-channel"
		channelName := "test-channel-name"

		// Create channel and set its name
		queries := schema.New(bot.DB)
		_, err := queries.AddChannel(ctx, schema.AddChannelParams{
			ChannelID: channelID,
			Attrs:     dto.ChannelAttrs{Name: channelName},
		})
		require.NoError(t, err)

		// Create test report data
		now := time.Now().UTC()
		reportData := dto.ReportData{
			ChannelName: channelName,
			WeekRange: dto.DateRange{
				Start: now.AddDate(0, 0, -7),
				End:   now,
			},
			Incidents: []dto.Incident{
				{
					Severity:    "P0",
					Count:       1,
					TotalTime:   time.Hour,
					AverageTime: time.Hour,
				},
			},
			TopAlerts: []dto.Alert{
				{
					Name:        "Test Alert",
					Count:       1,
					LastSeen:    now,
					AverageTime: time.Hour,
				},
			},
		}
		// Store the report
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
			ReportData: reportData,
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
		require.Equal(t, reportData.ChannelName, detailedReport.ReportData.ChannelName)
		require.Equal(t, reportData.WeekRange.Start.Unix(), detailedReport.ReportData.WeekRange.Start.Unix())
		require.Equal(t, reportData.WeekRange.End.Unix(), detailedReport.ReportData.WeekRange.End.Unix())
		require.Equal(t, len(reportData.Incidents), len(detailedReport.ReportData.Incidents))
		require.Equal(t, len(reportData.TopAlerts), len(detailedReport.ReportData.TopAlerts))
	})

	t.Run("enforces unique constraint on time period", func(t *testing.T) {
		channelID := "test-channel-2"
		channelName := "test-channel-2-name"

		// Create channel with name
		queries := schema.New(bot.DB)
		_, err := queries.AddChannel(ctx, schema.AddChannelParams{
			ChannelID: channelID,
			Attrs:     dto.ChannelAttrs{Name: channelName},
		})
		require.NoError(t, err)

		now := time.Now().UTC()
		reportData := dto.ReportData{
			ChannelName: channelName,
			WeekRange: dto.DateRange{
				Start: now.AddDate(0, 0, -7),
				End:   now,
			},
		}

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
			ReportData: reportData,
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
			ReportData: reportData,
		})
		require.Error(t, err) // Should fail due to unique constraint
	})

	t.Run("cascade deletes reports when channel is deleted", func(t *testing.T) {
		channelID := "test-channel-3"
		channelName := "test-channel-3-name"

		// Create channel with name
		queries := schema.New(bot.DB)
		_, err := queries.AddChannel(ctx, schema.AddChannelParams{
			ChannelID: channelID,
			Attrs:     dto.ChannelAttrs{Name: channelName},
		})
		require.NoError(t, err)

		now := time.Now().UTC()
		reportData := dto.ReportData{
			ChannelName: channelName,
			WeekRange: dto.DateRange{
				Start: now.AddDate(0, 0, -7),
				End:   now,
			},
		}

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
			ReportData: reportData,
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
