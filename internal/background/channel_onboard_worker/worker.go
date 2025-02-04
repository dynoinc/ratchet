package channel_onboard_worker

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type ChannelOnboardWorker struct {
	river.WorkerDefaults[background.ChannelOnboardWorkerArgs]

	slackClient *slack.Client
	bot         *internal.Bot
}

func New(slackClient *slack.Client, bot *internal.Bot) *ChannelOnboardWorker {
	return &ChannelOnboardWorker{
		slackClient: slackClient,
		bot:         bot,
	}
}

func (w *ChannelOnboardWorker) Work(ctx context.Context, job *river.Job[background.ChannelOnboardWorkerArgs]) error {
	channelInfo, err := w.slackClient.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID: job.Args.ChannelID,
	})
	if err != nil {
		return fmt.Errorf("getting channel info for channel ID %s: %w", job.Args.ChannelID, err)
	}

	params := &slack.GetConversationHistoryParameters{
		ChannelID: job.Args.ChannelID,
		Latest:    slack_integration.TimeToTs(time.Now()),
		Limit:     1000,
	}

	var messages []slack.Message
	for {
		history, err := w.slackClient.GetConversationHistory(params)
		if err != nil {
			return fmt.Errorf("getting conversation history for channel ID %s: %w", job.Args.ChannelID, err)
		}

		messages = append(messages, history.Messages...)
		if !history.HasMore || len(messages) >= 10 {
			break
		}

		params.Cursor = history.ResponseMetadata.Cursor
		params.Latest = history.Messages[len(history.Messages)-1].Timestamp
	}

	addMessageParams := make([]schema.AddMessageParams, len(messages))
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
	}

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := w.bot.UpdateChannel(ctx, tx, schema.UpdateChannelAttrsParams{
		ID: job.Args.ChannelID,
		Attrs: dto.ChannelAttrs{
			Name: channelInfo.Name,
		},
	}); err != nil {
		return fmt.Errorf("updating channel info for channel ID %s: %w", job.Args.ChannelID, err)
	}

	if err = w.bot.AddMessage(ctx, tx, addMessageParams); err != nil {
		return fmt.Errorf("adding messages to channel %s: %w", job.Args.ChannelID, err)
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}
