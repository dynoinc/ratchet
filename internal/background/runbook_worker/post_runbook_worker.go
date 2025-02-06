package runbook_worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
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

	serviceName := msg.IncidentAction.Service
	alertName := msg.IncidentAction.Alert

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
	}

	// TODO: Use lexical+semantic search to get the most relevant updates
	updates, err := schema.New(w.bot.DB).GetLatestServiceUpdates(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("getting latest service updates: %w", err)
	}

	if len(updates) > 0 {
		updatesMessage := "Recent activity:\n"
		for _, update := range updates {
			updatesMessage += fmt.Sprintf("- %s (%s)\n", update.Attrs.Message.Text, update.Attrs.Message.User)
		}

		runbookMessage = fmt.Sprintf("%s\n\n%s", runbookMessage, updatesMessage)
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

func getRunbook(ctx context.Context, bot *internal.Bot, serviceName, alertName string) (string, error) {
	runbook, err := schema.New(bot.DB).GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: serviceName,
		AlertName:   alertName,
	})
	if err != nil {
		return "", fmt.Errorf("getting runbook: %w", err)
	}

	if runbook.Attrs.Runbook == "" {
		return "No runbook found for this alert", nil
	}

	return runbook.Attrs.Runbook, nil
}
