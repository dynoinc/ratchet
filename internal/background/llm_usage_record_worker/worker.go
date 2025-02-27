package llm_usage_record_worker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
)

type Worker struct {
	river.WorkerDefaults[background.LLMUsageRecordWorkerArgs]
	bot *internal.Bot
}

func New(bot *internal.Bot) *Worker {
	return &Worker{
		bot: bot,
	}
}

func (w *Worker) Work(ctx context.Context, job *river.Job[background.LLMUsageRecordWorkerArgs]) error {
	args := job.Args

	// Record the usage directly using the bot
	if err := w.bot.RecordLLMUsage(ctx, args); err != nil {
		return fmt.Errorf("recording LLM usage: %w", err)
	}

	slog.InfoContext(ctx, "Recorded LLM usage",
		"model", args.Model,
		"operation", args.OperationType,
		"status", args.Status)

	return nil
}

func (w *Worker) Kind() string {
	return "llm_usage_record"
}
