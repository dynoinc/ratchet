package runbook

import (
	"context"
	"fmt"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/recent_activity"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/slack-go/slack"
)

type Handler struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
}

func New(bot *internal.Bot, slackIntegration slack_integration.Integration, llmClient llm.Client) *Handler {
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
	if msg.IncidentAction.Action != dto.ActionOpenIncident {
		return nil
	}

	qtx := schema.New(h.bot.DB)
	runbook, err := Get(ctx, qtx, h.llmClient, msg.IncidentAction.Service, msg.IncidentAction.Alert, h.slackIntegration.BotUserID())
	if err != nil {
		return err
	}

	if len(runbook) == 0 {
		// do not paste runbook if it's empty
		return nil
	}

	updates, err := recent_activity.Get(ctx, qtx, h.llmClient, msg.IncidentAction.Service, msg.IncidentAction.Alert, time.Hour, h.slackIntegration.BotUserID())
	if err != nil {
		return fmt.Errorf("getting updates: %w", err)
	}

	blocks := format(msg.IncidentAction.Service, msg.IncidentAction.Alert, runbook, updates)
	return Post(ctx, h.slackIntegration, channelID, slackTS, blocks...)
}

func format(
	serviceName, alertName, runbookMessage string,
	updates []recent_activity.Activity,
) []slack.Block {
	// Create blocks array and add header
	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("Runbook for %s - %s", serviceName, alertName), false, false),
		),
		slack.NewDividerBlock(),
	}

	// Add runbook content
	if len(runbookMessage) > 0 {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Runbook:*", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, runbookMessage, false, false),
				nil, nil,
			),
		)
	} else {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "_No runbook found for this alert_", false, false),
			nil, nil,
		))
	}

	// Add divider before updates section
	blocks = append(blocks, slack.NewDividerBlock())

	if len(updates) > 0 {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Recent Activity:*", false, false),
				nil, nil,
			),
		)

		for _, update := range updates {
			messageLink := fmt.Sprintf("https://slack.com/app_redirect?channel=%s&message_ts=%s",
				update.ChannelID, update.Ts)
			updateText := fmt.Sprintf("• <%s|%s> (%s)",
				messageLink, update.Attrs.Message.Text, update.Attrs.Message.User)

			blocks = append(blocks, slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, updateText, false, false),
				nil, nil,
			))
		}
	} else {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "_No recent activity found in the last hour_", false, false),
			nil, nil,
		))
	}

	// Add divider before footer
	blocks = append(blocks, slack.NewDividerBlock())

	// Add footer
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType,
			fmt.Sprintf("_Generated by Ratchet at %s_", time.Now().Format(time.RFC1123)),
			false, false,
		),
		nil, nil,
	))

	return blocks
}
