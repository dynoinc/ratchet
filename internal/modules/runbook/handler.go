package runbook

import (
	"context"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Handler struct {
	bot              *internal.Bot
	slackIntegration *slack_integration.Integration
	llmClient        *llm.Client
}

func New(bot *internal.Bot, slackIntegration *slack_integration.Integration, llmClient *llm.Client) *Handler {
	return &Handler{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}
}

func (h *Handler) Name() string {
	return "runbook"
}

func (h *Handler) Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	qtx := schema.New(h.bot.DB)
	blocks, err := Get(ctx, qtx, h.llmClient, msg.IncidentAction.Service, msg.IncidentAction.Alert, h.slackIntegration.BotUserID)
	if err != nil {
		return err
	}

	return Post(ctx, h.slackIntegration, channelID, slackTS, blocks...)
}
