package ingestion_worker

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"
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
	log.Printf("ingesting messages for channel %s", j.Args.ChannelID)
	params := slack.GetConversationHistoryParameters{
		ChannelID: j.Args.ChannelID,
		Limit:     100,
		Oldest:    j.Args.StartTime.Format(time.RFC3339),
		Latest:    j.Args.EndTime.Format(time.RFC3339),
	}

	for {
		messages, err := w.slackClient.GetConversationHistory(&params)
		if err != nil {
			log.Printf("error getting conversation history: %v", err)
			return err
		}
		log.Printf("got %d messages", len(messages.Messages))

		for _, msg := range messages.Messages {
			// Attempt to add each message, ignoring duplicate errors
			err := w.bot.AddMessage(ctx, j.Args.ChannelID, msg.Timestamp, dto.MessageAttrs{Message: msg})
			if err != nil && !isDuplicateError(err) {
				return err
			}
		}

		if messages.HasMore {
			params.Cursor = messages.Messages[len(messages.Messages)-1].Timestamp
		} else {
			break
		}
	}
	return nil
}

// helper function to check for duplicate message errors
func isDuplicateError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == pgerrcode.UniqueViolation
	}
	return false
}
