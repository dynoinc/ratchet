package classifier_worker

import (
	"context"
	"encoding/json"
	"log"

	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
)

type DevClassifierWorker struct {
	river.WorkerDefaults[background.ClassifierArgs]

	bot *internal.Bot
}

func NewDev(ctx context.Context, bot *internal.Bot) river.Worker[background.ClassifierArgs] {
	return &DevClassifierWorker{bot: bot}
}

func (w *DevClassifierWorker) Work(ctx context.Context, job *river.Job[background.ClassifierArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		return err
	}

	text := msg.Attrs.Message.Text
	if msg.Attrs.Upstream.Text != "" {
		text = msg.Attrs.Upstream.Text
	}

	var action IncidentAction
	if err := json.Unmarshal([]byte(text), &action); err != nil {
		return nil
	}

	log.Printf("processing incident action: %v\n", action)
	return processIncidentAction(ctx, w.bot, msg, &action)
}
