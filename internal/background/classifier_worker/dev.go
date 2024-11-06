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

	var text string
	if msg.Attrs.Upstream != nil {
		text = msg.Attrs.Upstream.Text
	} else {
		text = msg.Attrs.Message.Text
	}

	var action IncidentAction
	if err := json.Unmarshal([]byte(text), &action); err == nil {
		return processIncidentAction(ctx, w.bot, msg, &action)
	}

	subType := ""
	if msg.Attrs.Upstream != nil {
		subType = msg.Attrs.Upstream.SubType
	} else {
		subType = msg.Attrs.Message.SubType
	}

	if subType == "bot_message" {
		botName := ""
		if msg.Attrs.Upstream != nil {
			botName = msg.Attrs.Upstream.Username
		} else {
			botName = msg.Attrs.Message.Username
		}

		return w.bot.TagAsBotNotification(ctx, msg.ChannelID, msg.SlackTs, botName)
	}

	userID := ""
	if msg.Attrs.Upstream != nil {
		userID = msg.Attrs.Upstream.User
	} else {
		userID = msg.Attrs.Message.User
	}

	return w.bot.TagAsUserMessage(ctx, msg.ChannelID, msg.SlackTs, userID)
}
