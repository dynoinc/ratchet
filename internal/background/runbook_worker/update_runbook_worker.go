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
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		return fmt.Errorf("getting message: %w", err)
	}

	// get thread messages
	threadMsgs, err := schema.New(w.bot.DB).GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: job.Args.ChannelID,
		ParentTs:  job.Args.SlackTS,
	})
	if err != nil {
		return fmt.Errorf("getting thread messages: %w", err)
	}

	// get current runbook
	runbook, err := schema.New(w.bot.DB).GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: msg.IncidentAction.Service,
		AlertName:   msg.IncidentAction.Alert,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("getting runbook: %w", err)
	}

	// ask LLM to update the existing runbook with the info from new messages
	updatedRunbook, err := w.llmClient.UpdateRunbook(ctx, runbook, msg, threadMsgs)
	if err != nil {
		return fmt.Errorf("updating runbook: %w", err)
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := schema.New(tx)
	if _, err := qtx.CreateRunbook(ctx, dto.RunbookAttrs{
		ServiceName: msg.IncidentAction.Service,
		AlertName:   msg.IncidentAction.Alert,
		Runbook:     updatedRunbook,
	}); err != nil {
		return fmt.Errorf("creating runbook: %w", err)
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
