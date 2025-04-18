package runbook

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/recent_activity"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
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

func (h *Handler) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	if msg.IncidentAction.Action != dto.ActionOpenIncident {
		return nil
	}

	qtx := schema.New(h.bot.DB)
	runbook, err := Get(ctx, qtx, h.llmClient, msg.IncidentAction.Service, msg.IncidentAction.Alert, h.slackIntegration.BotUserID())
	if err != nil {
		return err
	}

	if runbook == nil {
		return nil
	}

	updates, err := recent_activity.Get(ctx, qtx, h.llmClient, runbook.LexicalSearchQuery, runbook.SemanticSearchQuery, time.Hour, h.slackIntegration.BotUserID())
	if err != nil {
		return fmt.Errorf("getting updates: %w", err)
	}

	blocks := Format(msg.IncidentAction.Service, msg.IncidentAction.Alert, runbook, updates)
	return h.slackIntegration.PostThreadReply(ctx, channelID, slackTS, blocks...)
}

// Format creates the Slack message blocks for a runbook response
func Format(service, alert string, runbook *llm.RunbookResponse, updates []recent_activity.Activity) []slack.Block {
	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("Runbook: %s/%s", service, alert), true, false),
		),
	}

	if runbook != nil {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("*Alert Overview*\n%s", runbook.AlertOverview), false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Historical Root Causes*", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(
					slack.MarkdownType,
					func() string {
						if len(runbook.HistoricalRootCauses) == 0 {
							return "No historical root causes found"
						}
						return "• " + strings.Join(runbook.HistoricalRootCauses, "\n• ")
					}(),
					false,
					false,
				),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Resolution Steps*", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(
					slack.MarkdownType,
					func() string {
						if len(runbook.ResolutionSteps) == 0 {
							return "No resolution steps available"
						}
						return "• " + strings.Join(runbook.ResolutionSteps, "\n• ")
					}(),
					false,
					false,
				),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Lexical Search Query*\n"+runbook.LexicalSearchQuery, false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Semantic Search Query*\n"+runbook.SemanticSearchQuery, false, false),
				nil, nil,
			),
			slack.NewDividerBlock(),
		)
	}

	if len(updates) > 0 {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Recent Slack Activity Relevant To Alert:*", false, false),
				nil, nil,
			),
		)

		for _, update := range updates {
			messageLink := fmt.Sprintf("• https://slack.com/archives/%s/p%s",
				update.ChannelID, strings.Replace(update.Ts, ".", "", 1))

			blocks = append(blocks, slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, messageLink, false, false),
				nil, nil,
			))
		}
		blocks = append(blocks, slack.NewDividerBlock())
	}

	// Add standardized signature block
	blocks = append(blocks, slack_integration.CreateSignatureBlock("Runbook")...)

	return blocks
}
