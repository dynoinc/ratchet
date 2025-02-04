package runbook_worker

import (
	"context"
	"errors"
	"fmt"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"
)

type postRunbookWorker struct {
	river.WorkerDefaults[background.PostRunbookWorkerArgs]

	bot          *internal.Bot
	slackClient  *slack.Client
	devChannelID string
}

func NewPostRunbookWorker(bot *internal.Bot, slackClient *slack.Client, devChannelID string) *postRunbookWorker {
	return &postRunbookWorker{
		bot:          bot,
		slackClient:  slackClient,
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

	// get latest 5 notifications for the service by bots
	updates, err := schema.New(w.bot.DB).GetLatestServiceUpdates(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("getting latest service updates: %w", err)
	}

	// post a message to the thread with the runbook and updates formatted nicely.
	updatesMessage := "No updates found"
	if len(updates) > 0 {
		updatesMessage = "Latest bot updates:\n"
		for _, update := range updates {
			updatesMessage += fmt.Sprintf("- %s (%s)\n", update.Attrs.Message.Text, update.Attrs.Message.User)
		}
	}

	runbookMessage := "No runbook found for this alert"
	if runbook.Attrs.Runbook != "" {
		runbookMessage = fmt.Sprintf("Runbook: %s", runbook.Attrs.Runbook)
	}
	runbookMessage = fmt.Sprintf("%s\n\n%s", runbookMessage, updatesMessage)

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
