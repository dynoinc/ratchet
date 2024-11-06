package background

import (
	"context"
	"fmt"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"
)

// SetupWeeklyReportJob configures the weekly report periodic job
func SetupWeeklyReportJob(ctx context.Context, db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) error {
	// Schedule for every Monday at 9 AM PST
	// TODO: make this configurable per channel
	schedule, err := cron.ParseStandard("0 9 * * 1")
	if err != nil {
		return fmt.Errorf("error parsing cron schedule: %w", err)
	}

	channels, err := schema.New(db).GetSlackChannels(ctx)
	if err != nil {
		return fmt.Errorf("error getting slack channels: %w", err)
	}

	for _, channel := range channels {
		constructor := func() (river.JobArgs, *river.InsertOpts) {
			return &WeeklyReportJobArgs{ChannelID: channel.ChannelID}, nil
		}

		periodicJob := river.NewPeriodicJob(schedule, constructor, &river.PeriodicJobOpts{
			RunOnStart: false,
		})

		riverClient.PeriodicJobs().Add(periodicJob)
	}

	return nil
}
