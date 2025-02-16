package channel_onboard_worker

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type ChannelOnboardWorker struct {
	river.WorkerDefaults[background.ChannelOnboardWorkerArgs]

	bot              *internal.Bot
	slackIntegration *slack_integration.Integration
}

func New(bot *internal.Bot, slackIntegration *slack_integration.Integration) *ChannelOnboardWorker {
	return &ChannelOnboardWorker{
		bot:              bot,
		slackIntegration: slackIntegration,
	}
}

func (w *ChannelOnboardWorker) Timeout(job *river.Job[background.ChannelOnboardWorkerArgs]) time.Duration {
	return 5 * time.Minute
}

func (w *ChannelOnboardWorker) Work(ctx context.Context, job *river.Job[background.ChannelOnboardWorkerArgs]) error {
	channelInfo, err := w.slackIntegration.GetConversationInfo(ctx, job.Args.ChannelID)
	if err != nil {
		return fmt.Errorf("getting channel info for channel ID %s: %w", job.Args.ChannelID, err)
	}

	lastNMsgs := job.Args.LastNMsgs
	if lastNMsgs == 0 {
		lastNMsgs = 1000
	}

	messages, err := w.slackIntegration.GetConversationHistory(ctx, job.Args.ChannelID, lastNMsgs)
	if err != nil {
		return fmt.Errorf("getting conversation history for channel ID %s: %w", job.Args.ChannelID, err)
	}

	addMessageParams := make([]schema.AddMessageParams, len(messages))
	var backfillThreadInsertParams []river.InsertManyParams
	for i, message := range messages {
		addMessageParams[i] = schema.AddMessageParams{
			ChannelID: job.Args.ChannelID,
			Ts:        message.Timestamp,
			Attrs: dto.MessageAttrs{
				Message: dto.SlackMessage{
					SubType:     message.SubType,
					Text:        message.Text,
					User:        message.User,
					BotID:       message.BotID,
					BotUsername: message.Username,
				},
			},
		}

		if message.ReplyCount > 0 {
			backfillThreadInsertParams = append(backfillThreadInsertParams, river.InsertManyParams{
				Args: background.BackfillThreadWorkerArgs{
					ChannelID: job.Args.ChannelID,
					SlackTS:   message.Timestamp,
				},
			})
		}
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := w.bot.UpdateChannel(ctx, tx, schema.UpdateChannelAttrsParams{
		ID: job.Args.ChannelID,
		Attrs: dto.ChannelAttrs{
			Name:             channelInfo.Name,
			OnboardingStatus: dto.OnboardingStatusFinished,
		},
	}); err != nil {
		return fmt.Errorf("updating channel info for channel ID %s: %w", job.Args.ChannelID, err)
	}

	if err = w.bot.AddMessage(ctx, tx, addMessageParams, internal.SourceBackfill); err != nil {
		return fmt.Errorf("adding messages to channel %s: %w", job.Args.ChannelID, err)
	}

	if len(backfillThreadInsertParams) > 0 {
		client := river.ClientFromContext[pgx.Tx](ctx)
		if _, err := client.InsertManyTx(ctx, tx, backfillThreadInsertParams); err != nil {
			return fmt.Errorf("inserting backfill thread insert params: %w", err)
		}
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
