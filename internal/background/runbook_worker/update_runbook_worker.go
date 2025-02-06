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
	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := schema.New(w.bot.DB).WithTx(tx)
	if _, err = updateRunbook(ctx, job.Args.Service, job.Args.Alert, job.Args.ForceRecreate, qtx, w.llmClient); err != nil {
		return fmt.Errorf("updating runbook: %w", err)
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}

func updateRunbook(
	ctx context.Context,
	serviceName, alertName string,
	forceRecreate bool,
	qtx *schema.Queries,
	llmClient *llm.Client,
) (string, error) {
	msgs, err := qtx.GetThreadMessagesByServiceAndAlert(ctx, schema.GetThreadMessagesByServiceAndAlertParams{
		Service: serviceName,
		Alert:   alertName,
	})
	if err != nil {
		return "", fmt.Errorf("getting thread messages: %w", err)
	}

	// get current runbook
	runbook, err := qtx.GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: serviceName,
		AlertName:   alertName,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("getting runbook: %w", err)
	}

	var updatedRunbook string
	if runbook == (schema.IncidentRunbook{}) || forceRecreate {
		// create new runbook from scratch
		updatedRunbook, err = llmClient.CreateRunbook(ctx, serviceName, alertName, msgs)
		if err != nil {
			return "", fmt.Errorf("creating runbook: %w", err)
		}
	} else {
		// update existing runbook with new messages
		updatedRunbook, err = llmClient.UpdateRunbook(ctx, runbook, msgs)
		if err != nil {
			return "", fmt.Errorf("updating runbook: %w", err)
		}
	}

	if updatedRunbook != "" {
		if _, err := qtx.CreateRunbook(ctx, dto.RunbookAttrs{
			ServiceName: serviceName,
			AlertName:   alertName,
			Runbook:     updatedRunbook,
		}); err != nil {
			return "", fmt.Errorf("writing updated runbook: %w", err)
		}
	}

	return updatedRunbook, nil
}
