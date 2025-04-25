package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/docrag"
	"github.com/dynoinc/ratchet/internal/modules/docupdate"
	"github.com/dynoinc/ratchet/internal/modules/report"
	"github.com/dynoinc/ratchet/internal/modules/usage"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type cmd string

const (
	cmdNone                cmd = "none"
	cmdPostWeeklyReport    cmd = "weekly_report"
	cmdPostUsageReport     cmd = "usage_report"
	cmdLookupDocumentation cmd = "lookup_documentation"
	cmdUpdateDocumentation cmd = "update_documentation"
)

var (
	sampleMessages = map[string][]string{
		string(cmdNone): {
			"how are you doing?",
			"what's the weather like?",
			"can you help me with something?",
		},
		string(cmdPostWeeklyReport): {
			"generate weekly incident report for this channel",
			"post report",
			"what's the status report",
			"show me the weekly summary",
		},
		string(cmdPostUsageReport): {
			"show ratchet bot usage statistics",
			"post usage report",
			"how many people are using the bot?",
		},
		string(cmdLookupDocumentation): {
			"lookup documentation",
			"what does the documentation say",
			"reference the docs",
		},
		string(cmdUpdateDocumentation): {
			"update the documentation",
			"update the docs",
		},
	}
)

type Commands struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
	docUpdater       *docupdate.DocUpdater
}

func New(
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
	docUpdater *docupdate.DocUpdater,
) *Commands {
	return &Commands{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
		docUpdater:       docUpdater,
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

	result, err := c.llmClient.ClassifyCommand(ctx, text, sampleMessages)
	if err != nil {
		return cmdNone, err
	}

	result = strings.TrimSpace(strings.ToLower(result))
	switch result {
	case "weekly_report":
		return cmdPostWeeklyReport, nil
	case "usage_report":
		return cmdPostUsageReport, nil
	case "update_documentation":
		return cmdUpdateDocumentation, nil
	default:
		return cmdNone, nil
	}
}

func (c *Commands) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	return c.handleMessage(ctx, channelID, slackTS, msg)
}

func (c *Commands) OnThreadMessage(ctx context.Context, channelID string, slackTS string, parentTS string, msg dto.MessageAttrs) error {
	return c.handleMessage(ctx, channelID, parentTS, msg)
}

func (c *Commands) handleMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
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
	case cmdLookupDocumentation:
		return docrag.Respond(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID, slackTS)
	case cmdUpdateDocumentation:
		if c.docUpdater != nil {
			return c.docUpdater.Update(ctx, channelID, slackTS, text)
		}
	case cmdNone: // nothing to do
	}

	return nil
}
