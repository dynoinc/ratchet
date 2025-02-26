package llm_usage_cleanup_worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Config struct {
	// Default retention period for LLM usage data, in days
	DefaultRetentionDays int `split_words:"true" default:"90"`
}

// Worker is a background worker that cleans up old LLM usage data
type Worker struct {
	db    schema.DBTX
	cfg   Config
}

// New creates a new LLM usage cleanup worker
func New(cfg Config, db schema.DBTX) *Worker {
	return &Worker{
		db:  db,
		cfg: cfg,
	}
}

// Work implements river.Worker
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

	// Execute the cleanup query
	query := `DELETE FROM llm_usage_v1 WHERE created_at < $1`
	result, err := w.db.Exec(ctx, query, cutoffTz)
	if err != nil {
		return fmt.Errorf("deleting old LLM usage data: %w", err)
	}

	rowsDeleted := result.RowsAffected()
	slog.InfoContext(ctx, "Cleaned up old LLM usage data", "deleted_rows", rowsDeleted, "retention_days", retentionDays)

	return nil
}

// Schedule periodically cleans up old LLM usage data
func (w *Worker) Schedule(ctx context.Context, client *river.Client[pgx.Tx]) error {
	// Schedule a job to run daily at midnight
	if _, err := client.Insert(ctx, background.LLMUsageCleanupWorkerArgs{
		RetentionDays: w.cfg.DefaultRetentionDays,
	}, &river.InsertOpts{
		ScheduledAt: river.ScheduleDaily(0, 0), // Run at midnight
	}); err != nil {
		return fmt.Errorf("scheduling LLM usage cleanup job: %w", err)
	}

	return nil
}