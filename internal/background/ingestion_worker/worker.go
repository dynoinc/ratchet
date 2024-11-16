package ingestion_worker

import (
	"context"
	"fmt"
	"log"
	"sort"
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
	latest := fmt.Sprintf("%d.%09d", time.Now().Unix(), 0)
	params := slack.GetConversationHistoryParameters{
		ChannelID: j.Args.ChannelID,
		Oldest:    j.Args.SlackTSWatermark,
		Latest:    latest,
	}

	var messages []slack.Message
	for {
		history, err := w.slackClient.GetConversationHistory(&params)
		if err != nil {
			return fmt.Errorf("error getting conversation history: %w", err)
		}

		messages = append(messages, history.Messages...)
		if !history.HasMore {
			break
		}

		params.Cursor = history.ResponseMetadata.Cursor
	}

	// Slack returns messages in reverse chronological order.
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})

	log.Printf("Adding %d messages from %s", len(messages), j.Args.ChannelID)
	for _, message := range messages {
		log.Printf("Adding message %s: %v", message.Timestamp, message.Text)
	}

	if err := w.bot.AddMessages(ctx, j.Args.ChannelID, messages, latest); err != nil {
		return fmt.Errorf("error adding history: %w", err)
	}

	return nil
}
