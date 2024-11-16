package ingestion_worker

import (
	"context"
	"fmt"
	"log"
	"time"

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
	params := slack.GetConversationHistoryParameters{
		ChannelID: j.Args.ChannelID,
		Limit:     2,
		Oldest:    j.Args.OldestSlackTS,
	}

	messages, err := w.slackClient.GetConversationHistory(&params)
	if err != nil {
		return fmt.Errorf("error getting conversation history: %w", err)
	}

	log.Printf("Processing %d messages from %s", len(messages.Messages), j.Args.ChannelID)
	for _, message := range messages.Messages {
		log.Printf("Processing message %s: %v", message.Timestamp, message.Text)
	}

	if err := w.bot.AddMessages(ctx, j.Args.ChannelID, messages.Messages); err != nil {
		return fmt.Errorf("error adding messages: %w", err)
	}

	scheduledAt := time.Time{}
	if !messages.HasMore {
		scheduledAt = time.Now().Add(time.Minute)
	}

	oldestSlackTS := j.Args.OldestSlackTS
	if len(messages.Messages) > 0 {
		oldestSlackTS = messages.Messages[len(messages.Messages)-1].Timestamp
	}

	if _, err := w.bot.RiverClient.Insert(
		ctx,
		background.MessagesIngestionWorkerArgs{
			ChannelID:     j.Args.ChannelID,
			OldestSlackTS: oldestSlackTS,
		},
		&river.InsertOpts{
			UniqueOpts:  river.UniqueOpts{ByArgs: true},
			ScheduledAt: scheduledAt,
		},
	); err != nil {
		return err
	}

	return nil
}
