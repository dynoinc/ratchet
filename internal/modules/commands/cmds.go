package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/report"
	"github.com/dynoinc/ratchet/internal/modules/usage"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type cmd int

const (
	cmdNone cmd = iota
	cmdPostWeeklyReport
	cmdPostUsageReport
)

type Commands struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
}

func New(
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) *Commands {
	return &Commands{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}
}

func (c *Commands) Name() string {
	return "commands"
}

func (c *Commands) findCommand(ctx context.Context, text string) (cmd, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return cmdNone, nil
	}

	result, err := c.llmClient.ClassifyCommand(ctx, text)
	if err != nil {
		return cmdNone, err
	}

	result = strings.TrimSpace(strings.ToLower(result))
	switch result {
	case "weekly_report":
		return cmdPostWeeklyReport, nil
	case "usage_report":
		return cmdPostUsageReport, nil
	default:
		return cmdNone, nil
	}
}

func (c *Commands) Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	botID := c.slackIntegration.BotUserID()
	text, found := strings.CutPrefix(msg.Message.Text, fmt.Sprintf("<@%s> ", botID))
	if !found {
		return nil
	}

	bestMatch, err := c.findCommand(ctx, text)
	if err != nil {
		return err
	}

	switch bestMatch {
	case cmdPostWeeklyReport:
		return report.Post(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID)
	case cmdPostUsageReport:
		return usage.Post(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID)
	case cmdNone: // nothing to do
	}

	return nil
}
