package classifier_worker

import (
	"context"
	"encoding/json"

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

	var action IncidentAction
	text := msg.Attrs.Message.Text
	if err := json.Unmarshal([]byte(text), &action); err == nil {
		return processIncidentAction(ctx, w.bot, msg, &action)
	}

	subType := msg.Attrs.Message.SubType
	if subType == "bot_message" {
		botName := msg.Attrs.Message.Username
		return w.bot.TagAsBotNotification(ctx, msg.ChannelID, msg.SlackTs, botName)
	} else {
		userID := msg.Attrs.Message.User
		return w.bot.TagAsUserMessage(ctx, msg.ChannelID, msg.SlackTs, userID)
	}
}
