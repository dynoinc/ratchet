package ingestion_worker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/slack_integration"
)

type messagesIngestionWorker struct {
	river.WorkerDefaults[background.MessagesIngestionWorkerArgs]

	bot         *internal.Bot
	slackClient *slack.Client
}

func New(bot *internal.Bot, slackClient *slack.Client) (*messagesIngestionWorker, error) {
	return &messagesIngestionWorker{bot: bot, slackClient: slackClient}, nil
}

func (w *messagesIngestionWorker) Work(ctx context.Context, j *river.Job[background.MessagesIngestionWorkerArgs]) error {
	// Need to make sure we watermark always advances forward.
	now := time.Now()
	latest := fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
	if latest <= j.Args.SlackTSWatermark {
		watermarkTS, err := slack_integration.TsToTime(j.Args.SlackTSWatermark)
		if err != nil {
			return fmt.Errorf("error converting slack timestamp to time: %w", err)
		}

		now = watermarkTS.Add(time.Millisecond)
		latest = fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
	}

	params := slack.GetConversationHistoryParameters{
		ChannelID: j.Args.ChannelID,
		Oldest:    j.Args.SlackTSWatermark,
		Latest:    latest,
	}

	var messages []slack.Message
	for {
		history, err := w.slackClient.GetConversationHistory(&params)
		if err != nil {
			return fmt.Errorf("error getting conversation history for channel %s: %w", j.Args.ChannelID, err)
		}

		messages = append(messages, history.Messages...)
		if !history.HasMore || len(messages) >= 1000 {
			break
		}

		params.Cursor = history.ResponseMetadata.Cursor
		params.Latest = history.Messages[len(history.Messages)-1].Timestamp
	}

	// Slack returns messages in reverse chronological order.
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp < messages[j].Timestamp
	})

	if len(messages) > 0 {
		slog.InfoContext(
			ctx, "Adding messages from channel",
			"count", len(messages),
			"channelID", j.Args.ChannelID,
		)

		for _, message := range messages {
			slog.InfoContext(
				ctx, "Adding message",
				"timestamp", message.Timestamp,
				"text", message.Text,
				"channelID", j.Args.ChannelID,
			)
		}
	}

	if err := w.bot.AddMessages(ctx, j.Args.ChannelID, messages, latest); err != nil {
		return fmt.Errorf("error adding history: %w", err)
	}

	return nil
}
