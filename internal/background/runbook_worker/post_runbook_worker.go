package runbook_worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"
)

type postRunbookWorker struct {
	river.WorkerDefaults[background.PostRunbookWorkerArgs]

	bot          *internal.Bot
	slackClient  *slack.Client
	llmClient    *llm.Client
	devChannelID string
}

func NewPostRunbookWorker(bot *internal.Bot, slackClient *slack.Client, llmClient *llm.Client, devChannelID string) *postRunbookWorker {
	return &postRunbookWorker{
		bot:          bot,
		slackClient:  slackClient,
		llmClient:    llmClient,
		devChannelID: devChannelID,
	}
}

func (w *postRunbookWorker) Work(ctx context.Context, job *river.Job[background.PostRunbookWorkerArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			return nil
		}

		return fmt.Errorf("getting message: %w", err)
	}

	serviceName := msg.Attrs.IncidentAction.Service
	alertName := msg.Attrs.IncidentAction.Alert

	runbook, err := schema.New(w.bot.DB).GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: serviceName,
		AlertName:   alertName,
	})
	if err != nil {
		return fmt.Errorf("getting runbook: %w", err)
	}

	runbookMessage := runbook.Attrs.Runbook
	if runbookMessage == "" {
		runbookMessage, err = updateRunbook(ctx, serviceName, alertName, false, schema.New(w.bot.DB), w.llmClient)
		if err != nil {
			return fmt.Errorf("updating runbook: %w", err)
		}

		runbookMessage += "\n\n"
	}

	updates, err := GetUpdates(ctx, w.bot.DB, w.llmClient, serviceName, alertName, time.Hour)
	if err != nil {
		return fmt.Errorf("getting updates: %w", err)
	}

	if len(updates) > 0 {
		updatesMessage := "Recent activity:\n"
		for _, update := range updates {
			updatesMessage += fmt.Sprintf("- %s (%s)\n", update.Attrs.Message.Text, update.Attrs.Message.User)
		}

		runbookMessage += updatesMessage
	}

	if runbookMessage == "" {
		return nil
	}

	channelID := job.Args.ChannelID
	msgOptions := []slack.MsgOption{slack.MsgOptionText(runbookMessage, false)}
	if w.devChannelID != "" {
		channelID = w.devChannelID
	} else {
		msgOptions = append(msgOptions, slack.MsgOptionTS(job.Args.SlackTS))
	}

	if _, _, err := w.slackClient.PostMessage(
		channelID,
		msgOptions...); err != nil {
		return fmt.Errorf("posting runbook message: %w", err)
	}

	return nil
}

func GetUpdates(ctx context.Context, db *pgxpool.Pool, llmClient *llm.Client, serviceName, alertName string, interval time.Duration) ([]schema.MessagesV2, error) {
	queryText := fmt.Sprintf("Service: %s, Alert: %s", serviceName, alertName)
	queryEmbedding, err := llmClient.GenerateEmbedding(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	embedding := pgvector.NewVector(queryEmbedding)
	updates, err := schema.New(db).GetLatestServiceUpdates(ctx, schema.GetLatestServiceUpdatesParams{
		QueryText:      queryText,
		QueryEmbedding: &embedding,
		Interval:       pgtype.Interval{Microseconds: interval.Microseconds(), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("getting latest service updates: %w", err)
	}

	return updates, nil
}
