package docrag

import (
	"context"
	"fmt"
	"strings"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type handler struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
}

func New(bot *internal.Bot, slackIntegration slack_integration.Integration, llmClient llm.Client) *handler {
	return &handler{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}
}

func (h *handler) Name() string {
	return "docrag"
}

func (h *handler) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	if msg.Message.BotID != "" || msg.Message.BotUsername != "" {
		return nil
	}

	botID := h.slackIntegration.BotUserID()
	if strings.HasPrefix(msg.Message.Text, fmt.Sprintf("<@%s> ", botID)) {
		return nil
	}

	channel, err := schema.New(h.bot.DB).GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("getting channel: %w", err)
	}
	if !channel.Attrs.DocResponsesEnabled {
		return nil
	}

	return Post(ctx, schema.New(h.bot.DB), h.llmClient, h.slackIntegration, channelID, slackTS, msg.Message.Text)
}
