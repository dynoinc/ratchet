package runbook_worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/riverqueue/river"
)

type postRunbookWorker struct {
	river.WorkerDefaults[background.PostRunbookWorkerArgs]

	bot              *internal.Bot
	slackIntegration *slack_integration.Integration
	llmClient        *llm.Client
}

func NewPostRunbookWorker(
	bot *internal.Bot,
	slackIntegration *slack_integration.Integration,
	llmClient *llm.Client,
) *postRunbookWorker {
	return &postRunbookWorker{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
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

	updates, err := GetUpdates(ctx, w.bot.DB, w.llmClient, serviceName, alertName, time.Hour, w.slackIntegration.BotUserID)
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

	return w.slackIntegration.PostThreadReply(ctx, job.Args.ChannelID, job.Args.SlackTS, runbookMessage)
}

func GetUpdates(
	ctx context.Context,
	db *pgxpool.Pool,
	llmClient *llm.Client,
	serviceName, alertName string,
	interval time.Duration,
	botID string,
) ([]schema.MessagesV2, error) {
	queryText := fmt.Sprintf("%s %s", serviceName, alertName)
	queryEmbedding, err := llmClient.GenerateEmbedding(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	embedding := pgvector.NewVector(queryEmbedding)
	updates, err := schema.New(db).GetLatestServiceUpdates(ctx, schema.GetLatestServiceUpdatesParams{
		QueryText:      queryText,
		QueryEmbedding: &embedding,
		Interval:       pgtype.Interval{Microseconds: interval.Microseconds(), Valid: true},
		BotID:          botID,
	})
	if err != nil {
		return nil, fmt.Errorf("getting latest service updates: %w", err)
	}

	messages := make([]schema.MessagesV2, len(updates))
	for i, update := range updates {
		var attrs dto.MessageAttrs
		if err := json.Unmarshal(update.Attrs, &attrs); err != nil {
			return nil, fmt.Errorf("unmarshalling message attrs: %w", err)
		}

		messages[i] = schema.MessagesV2{
			ChannelID: update.ChannelID,
			Ts:        update.Ts,
			Attrs:     attrs,
		}
	}

	return messages, nil
}
