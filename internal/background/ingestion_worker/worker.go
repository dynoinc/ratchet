package ingestion_worker

import (
	"context"
	"fmt"
	"log"

	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
)

type MessagesIngestionWorker struct {
	river.WorkerDefaults[background.MessagesIngestionWorkerArgs]

	bot         *internal.Bot
	slackClient *slack.Client
}

func New(bot *internal.Bot, slackClient *slack.Client) (*MessagesIngestionWorker, error) {
	return &MessagesIngestionWorker{bot: bot, slackClient: slackClient}, nil
}

func (w *MessagesIngestionWorker) Work(ctx context.Context, j *river.Job[background.MessagesIngestionWorkerArgs]) error {
	channel, err := w.bot.GetChannel(ctx, j.Args.ChannelID)
	if err != nil {
		return fmt.Errorf("error getting channel: %w", err)
	}

	params := slack.GetConversationHistoryParameters{
		ChannelID: j.Args.ChannelID,
		Limit:     100,
		Oldest:    channel.LatestSlackTs,
	}

	messages, err := w.slackClient.GetConversationHistory(&params)
	if err != nil {
		return fmt.Errorf("error getting conversation history: %w", err)
	}

	log.Printf("Processing %d messages from %s", len(messages.Messages), j.Args.ChannelID)
	if err := w.bot.AddMessages(ctx, j.Args.ChannelID, messages.Messages); err != nil {
		return fmt.Errorf("error adding messages: %w", err)
	}

	if messages.HasMore {
		if _, err := w.bot.RiverClient.Insert(
			ctx,
			background.MessagesIngestionWorkerArgs{ChannelID: j.Args.ChannelID},
			&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
		); err != nil {
			return err
		}
	}

	return nil
}
