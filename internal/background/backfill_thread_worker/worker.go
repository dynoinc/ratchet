package backfill_thread_worker

import (
	"context"
	"fmt"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/slack-go/slack"
)

type backfillThreadWorker struct {
	river.WorkerDefaults[background.BackfillThreadWorkerArgs]

	bot         *internal.Bot
	slackClient *slack.Client
}

func New(bot *internal.Bot, slackClient *slack.Client) *backfillThreadWorker {
	return &backfillThreadWorker{
		bot:         bot,
		slackClient: slackClient,
	}
}

func (w *backfillThreadWorker) Work(ctx context.Context, job *river.Job[background.BackfillThreadWorkerArgs]) error {
	params := &slack.GetConversationRepliesParameters{
		ChannelID: job.Args.ChannelID,
		Timestamp: job.Args.SlackTS,
	}

	var addThreadMessageParams []schema.AddThreadMessageParams
	for {
		threadMessages, hasMore, nextCursor, err := w.slackClient.GetConversationReplies(params)
		if err != nil {
			return fmt.Errorf("getting conversation replies for channel ID %s: %w", job.Args.ChannelID, err)
		}

		for _, threadMessage := range threadMessages {
			addThreadMessageParams = append(addThreadMessageParams, schema.AddThreadMessageParams{
				ChannelID: job.Args.ChannelID,
				ParentTs:  job.Args.SlackTS,
				Ts:        threadMessage.Timestamp,
				Attrs: dto.ThreadMessageAttrs{
					Message: dto.SlackMessage{
						Text:        threadMessage.Text,
						User:        threadMessage.User,
						BotID:       threadMessage.BotID,
						SubType:     threadMessage.SubType,
						BotUsername: threadMessage.Username,
					},
				},
			})
		}

		if !hasMore {
			break
		}

		params.Cursor = nextCursor
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if len(addThreadMessageParams) > 0 {
		if err = w.bot.AddThreadMessages(ctx, tx, addThreadMessageParams); err != nil {
			return fmt.Errorf("adding thread messages to channel %s: %w", job.Args.ChannelID, err)
		}
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
