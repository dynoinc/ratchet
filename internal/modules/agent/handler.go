package agent

import (
	"context"
	"log/slog"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/modules"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type agent struct {
	bot *internal.Bot
}

func New(bot *internal.Bot) modules.Handler {
	return &agent{
		bot: bot,
	}
}

func (w *agent) Name() string {
	return "agent"
}

func (w *agent) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	slog.InfoContext(ctx, "agent message", "channel_id", channelID, "slack_ts", slackTS, "msg", msg)
	return nil
}

func (w *agent) OnThreadMessage(ctx context.Context, channelID string, slackTS string, parentTS string, msg dto.MessageAttrs) error {
	slog.InfoContext(ctx, "agent thread message", "channel_id", channelID, "slack_ts", slackTS, "parent_ts", parentTS, "msg", msg)
	return nil
}
