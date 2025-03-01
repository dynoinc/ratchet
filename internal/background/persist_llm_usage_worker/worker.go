package persist_llm_usage_worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Worker struct {
	river.WorkerDefaults[background.PersistLLMUsageWorkerArgs]
	bot *internal.Bot
}

func New(bot *internal.Bot) *Worker {
	return &Worker{
		bot: bot,
	}
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.PersistLLMUsageWorkerArgs]) error {
	slog.InfoContext(ctx, "persisting LLM usage",
		"model", job.Args.Model)

	params := schema.AddLLMUsageParams{
		Input:  job.Args.Input,
		Output: job.Args.Output,
		Model:  job.Args.Model,
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := w.bot.RecordLLMUsage(ctx, tx, params); err != nil {
		return fmt.Errorf("recording LLM usage: %w", err)
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
