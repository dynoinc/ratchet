package commands

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/deployment_ops"
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
	cmdEnableAutoDocReply  cmd = "enable_auto_doc_reply"
	cmdDisableAutoDocReply cmd = "disable_auto_doc_reply"
	cmdLookupDocumentation cmd = "lookup_documentation"
	cmdUpdateDocumentation cmd = "update_documentation"
	cmdDeploymentOps       cmd = "deployment_ops"
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
		string(cmdEnableAutoDocReply): {
			"enable auto doc reply",
			"reply to questions with documentation automatically",
			"lookup documentation by default",
		},
		string(cmdDisableAutoDocReply): {
			"disable auto doc reply",
			"stop replying to questions automatically",
			"stop looking up documentation by default",
		},
		string(cmdLookupDocumentation): {
			"lookup documentation",
			"what does the documentation say",
			"reference the docs",
		},
		string(cmdUpdateDocumentation): {
			"update the documentation",
			"update the docs",
			"open a PR or a pull request",
			"fix the docs",
		},
		string(cmdDeploymentOps): {
			"list recent deployments for project some_project",
			"add ecap to deployment some_project/some_deployment",
			"roll back for deployment some_project/some_deployment",
		},
	}
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
	case "enable_auto_doc_reply":
		return cmdEnableAutoDocReply, nil
	case "disable_auto_doc_reply":
		return cmdDisableAutoDocReply, nil
	case "lookup_documentation":
		return cmdLookupDocumentation, nil
	case "update_documentation":
		return cmdUpdateDocumentation, nil
	case "deployment_ops":
		return cmdDeploymentOps, nil
	default:
		slog.DebugContext(ctx, "unknown command", "text", text, "command", result)
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
	case cmdEnableAutoDocReply:
		return c.bot.EnableAutoDocReply(ctx, channelID)
	case cmdDisableAutoDocReply:
		return c.bot.DisableAutoDocReply(ctx, channelID)
	case cmdLookupDocumentation:
		return docrag.Post(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID, slackTS, text)
	case cmdUpdateDocumentation:
		return docupdate.Post(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, c.bot.DocsConfig, channelID, slackTS, text)
	case cmdDeploymentOps:
		return deployment_ops.Post(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID, text)
	case cmdNone: // nothing to do
	}

	return nil
}
