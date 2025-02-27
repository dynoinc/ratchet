package llm_usage_cleanup_worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Config struct {
	// Default retention period for LLM usage data, in days
	DefaultRetentionDays int `split_words:"true" default:"90"`
}

// Worker is a background worker that cleans up old LLM usage data
type Worker struct {
	river.WorkerDefaults[background.LLMUsageCleanupWorkerArgs]

	bot *internal.Bot
	cfg Config
}

// Kind returns the kind of river job this worker handles
func (w *Worker) Kind() string {
	return "llm_usage_cleanup"
}

// New creates a new LLM usage cleanup worker
func New(cfg Config, bot *internal.Bot) *Worker {
	return &Worker{
		bot: bot,
		cfg: cfg,
	}
}

// Work implements river.Worker for background.LLMUsageCleanupWorkerArgs
func (w *Worker) Work(ctx context.Context, job *river.Job[background.LLMUsageCleanupWorkerArgs]) error {
	slog.InfoContext(ctx, "Cleaning up old LLM usage data")

	// Get the retention period from the job arguments or use the default
	retentionDays := w.cfg.DefaultRetentionDays
	if job.Args.RetentionDays > 0 {
		retentionDays = job.Args.RetentionDays
	}

	// Calculate the cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -retentionDays)
	cutoffTz := pgtype.Timestamptz{Time: cutoffDate, Valid: true}

	qtx := schema.New(w.bot.DB)
	// Execute the cleanup query
	if err := qtx.DeleteOldLLMUsage(ctx, cutoffTz); err != nil {
		return fmt.Errorf("deleting old LLM usage data: %w", err)
	}

	slog.InfoContext(ctx, "Cleaned up old LLM usage data", "retention_days", retentionDays)

	return nil
}

// Schedule periodically cleans up old LLM usage data
func (w *Worker) Schedule(ctx context.Context, client *river.Client[pgx.Tx]) error {
	// Schedule a job to run daily at midnight tomorrow
	scheduledTime := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)

	// Insert with scheduled time
	if _, err := client.Insert(ctx, background.LLMUsageCleanupWorkerArgs{
		RetentionDays: w.cfg.DefaultRetentionDays,
	}, &river.InsertOpts{
		ScheduledAt: scheduledTime,
	}); err != nil {
		return fmt.Errorf("scheduling LLM usage cleanup job: %w", err)
	}

	return nil
}
