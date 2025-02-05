package runbook_worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type updateRunbookWorker struct {
	river.WorkerDefaults[background.UpdateRunbookWorkerArgs]

	bot       *internal.Bot
	llmClient *llm.Client
}

func NewUpdateRunbookWorker(bot *internal.Bot, llmClient *llm.Client) *updateRunbookWorker {
	return &updateRunbookWorker{
		bot:       bot,
		llmClient: llmClient,
	}
}

func (w *updateRunbookWorker) Work(ctx context.Context, job *river.Job[background.UpdateRunbookWorkerArgs]) error {
	msgs, err := schema.New(w.bot.DB).GetThreadMessagesByServiceAndAlert(ctx, schema.GetThreadMessagesByServiceAndAlertParams{
		Service: job.Args.Service,
		Alert:   job.Args.Alert,
	})
	if err != nil {
		return fmt.Errorf("getting thread messages: %w", err)
	}

	// get current runbook
	runbook, err := schema.New(w.bot.DB).GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: job.Args.Service,
		AlertName:   job.Args.Alert,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("getting runbook: %w", err)
	}

	var updatedRunbook string
	if runbook == (schema.IncidentRunbook{}) {
		// create new runbook from scratch
		updatedRunbook, err = w.llmClient.CreateRunbook(ctx, job.Args.Service, job.Args.Alert, msgs)
		if err != nil {
			return fmt.Errorf("creating runbook: %w", err)
		}
	} else {
		// update existing runbook with new messages
		updatedRunbook, err = w.llmClient.UpdateRunbook(ctx, runbook, msgs)
		if err != nil {
			return fmt.Errorf("updating runbook: %w", err)
		}
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if updatedRunbook != "" {
		qtx := schema.New(tx)
		if _, err := qtx.CreateRunbook(ctx, dto.RunbookAttrs{
			ServiceName: job.Args.Service,
			AlertName:   job.Args.Alert,
			Runbook:     updatedRunbook,
		}); err != nil {
			return fmt.Errorf("writing updated runbook: %w", err)
		}
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
