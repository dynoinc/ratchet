package llm_usage_purge_worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Worker struct {
	river.WorkerDefaults[background.LLMUsagePurgeWorkerArgs]
	bot *internal.Bot
}

func New(bot *internal.Bot) *Worker {
	return &Worker{
		bot: bot,
	}
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.LLMUsagePurgeWorkerArgs]) error {
	retentionDays := job.Args.RetentionDays
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	slog.InfoContext(ctx, "purging LLM usage older than cutoff time",
		"retention_days", retentionDays,
		"cutoff_time", cutoffTime)

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := schema.New(w.bot.DB).WithTx(tx)

	// Convert cutoff time to pgtype.Timestamptz
	pgCutoffTime := pgtype.Timestamptz{
		Time:  cutoffTime,
		Valid: true,
	}

	rowsDeleted, err := qtx.PurgeLLMUsageOlderThan(ctx, pgCutoffTime)
	if err != nil {
		return fmt.Errorf("purging LLM usage: %w", err)
	}

	slog.InfoContext(ctx, "purged LLM usage entries", "count", rowsDeleted)

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
