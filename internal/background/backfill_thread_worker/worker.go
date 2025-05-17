package backfill_thread_worker

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type BackfillThreadWorker struct {
	river.WorkerDefaults[background.BackfillThreadWorkerArgs]

	bot              *internal.Bot
	slackIntegration slack_integration.Integration
}

func New(bot *internal.Bot, slackIntegration slack_integration.Integration) *BackfillThreadWorker {
	return &BackfillThreadWorker{
		bot:              bot,
		slackIntegration: slackIntegration,
	}
}

func (w *BackfillThreadWorker) NextRetry(job *river.Job[background.BackfillThreadWorkerArgs]) time.Time {
	// Most of the time failure is from slack API rate limiting, so back off aggressively.
	return time.Now().Add(30 * time.Second)
}

func (w *BackfillThreadWorker) Work(ctx context.Context, job *river.Job[background.BackfillThreadWorkerArgs]) error {
	messages, err := w.slackIntegration.GetConversationReplies(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		return fmt.Errorf("getting conversation replies for channel ID %s: %w", job.Args.ChannelID, err)
	}

	addThreadMessageParams := make([]schema.AddThreadMessageParams, len(messages))
	for i, message := range messages {
		reactions := make(map[string]int)
		for _, reaction := range message.Reactions {
			reactions[reaction.Name] = reaction.Count
		}

		addThreadMessageParams[i] = schema.AddThreadMessageParams{
			ChannelID: job.Args.ChannelID,
			ParentTs:  job.Args.SlackTS,
			Ts:        message.Timestamp,
			Attrs: dto.MessageAttrs{
				Message: dto.SlackMessage{
					Text:        message.Text,
					User:        message.User,
					BotID:       message.BotID,
					SubType:     message.SubType,
					BotUsername: message.Username,
				},
				Reactions: reactions,
			},
		}
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if len(addThreadMessageParams) > 0 {
		if err = w.bot.AddThreadMessages(ctx, tx, addThreadMessageParams, internal.SourceBackfill); err != nil {
			return fmt.Errorf("adding thread messages to channel %s: %w", job.Args.ChannelID, err)
		}
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
