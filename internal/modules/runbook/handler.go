package runbook

import (
	"context"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func New(bot *internal.Bot, slackIntegration *slack_integration.Integration, llmClient *llm.Client) modules.Handler {
	return modules.HandlerFunc(func(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
		if msg.IncidentAction.Action != dto.ActionOpenIncident {
			return nil
		}

		qtx := schema.New(bot.DB)
		blocks, err := Get(ctx, qtx, llmClient, msg.IncidentAction.Service, msg.IncidentAction.Alert, slackIntegration.BotUserID)
		if err != nil {
			return err
		}

		return Post(ctx, slackIntegration, channelID, slackTS, blocks...)
	})
}
