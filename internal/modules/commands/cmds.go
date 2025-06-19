package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
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
	"github.com/dynoinc/ratchet/internal/tools"
	"github.com/openai/openai-go"
	"github.com/slack-go/slack"
)

type Config struct {
	MCPServerURLs []string `envconfig:"MCP_SERVER_URLS"`
}

type Commands struct {
	config           Config
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
}

func New(
	config Config,
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) (*Commands, error) {
	for _, url := range config.MCPServerURLs {
		if _, err := exec.LookPath(url); err != nil {
			return nil, fmt.Errorf("looking up MCP server: %w", err)
		}
	}

	return &Commands{
		config:           config,
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}, nil
}

func (c *Commands) Name() string {
	return "commands"
}

func (c *Commands) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	channel, err := c.bot.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}

	force := channel.Attrs.AgentModeEnabled
	return c.handleMessage(ctx, channelID, slackTS, msg, force)
}

func (c *Commands) OnThreadMessage(ctx context.Context, channelID string, slackTS string, parentTS string, msg dto.MessageAttrs) error {
	return c.handleMessage(ctx, channelID, parentTS, msg, false)
}

func (c *Commands) handleMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs, force bool) error {
	botID := c.slackIntegration.BotUserID()
	text, found := strings.CutPrefix(msg.Message.Text, fmt.Sprintf("<@%s> ", botID))
	if !found && !force {
		return nil
	}

	// Use OpenAI API with tool calling
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(`You are a helpful assistant that manages Slack channels and provides various utilities. 
Use the available tools to help users with reports, documentation, and channel settings.
Always provide clear and concise responses about what actions you've taken.`),
			openai.UserMessage(text),
		},
		Tools: tools.Definitions(),
		Model: c.llmClient.Model(),
	}

	var response string
	for range 5 {
		openaiClient := c.llmClient.Client()
		completion, err := openaiClient.Chat.Completions.New(ctx, params)
		if err != nil {
			return fmt.Errorf("processing request: %w", err)
		}

		toolCalls := completion.Choices[0].Message.ToolCalls
		if len(toolCalls) == 0 {
			response = completion.Choices[0].Message.Content
			break
		}

		params.Messages = append(params.Messages, completion.Choices[0].Message.ToParam())
		for _, toolCall := range toolCalls {
			result, err := c.executeTool(ctx, toolCall.Function.Name, toolCall.Function.Arguments, channelID, slackTS)
			if err != nil {
				slog.ErrorContext(ctx, "Tool execution failed", "tool", toolCall.Function.Name, "error", err)
				return fmt.Errorf("tool execution failed: %w", err)
			}

			params.Messages = append(params.Messages, openai.ToolMessage(result, toolCall.ID))
		}
	}

	if response != "" {
		blocks := []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, response, false, false),
				nil, nil,
			),
		}
		blocks = append(blocks, slack_integration.CreateSignatureBlock("Commands")...)
		return c.slackIntegration.PostThreadReply(ctx, channelID, slackTS, blocks...)
	}

	return nil
}

func (c *Commands) executeTool(ctx context.Context, toolName string, args string, channelID, messageTS string) (string, error) {
	var arguments map[string]interface{}
	if err := json.Unmarshal([]byte(args), &arguments); err != nil {
		return "", fmt.Errorf("parsing tool arguments: %w", err)
	}

	switch toolName {
	case "generate_weekly_report":
		return report.Generate(ctx, schema.New(c.bot.DB), c.llmClient, channelID)

	case "generate_usage_report":
		return usage.Generate(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID)

	case "enable_auto_doc_reply":
		err := c.bot.EnableAutoDocReply(ctx, channelID)
		if err != nil {
			return "", err
		}
		return "Auto documentation replies have been enabled for this channel.", nil

	case "disable_auto_doc_reply":
		err := c.bot.DisableAutoDocReply(ctx, channelID)
		if err != nil {
			return "", err
		}
		return "Auto documentation replies have been disabled for this channel.", nil

	case "lookup_documentation":
		query := arguments["query"].(string)
		answer, links, err := docrag.Compute(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID, messageTS, query)
		if err != nil {
			return "", err
		}

		result := answer
		if len(links) > 0 {
			result += "\n\nSources:\n"
			for _, link := range links {
				result += fmt.Sprintf("- %s\n", link)
			}
		}
		return result, nil

	case "update_documentation":
		request := arguments["request"].(string)
		if c.bot.DocsConfig == nil {
			return "Documentation updates are not configured for this instance.", nil
		}

		url, err := docupdate.Generate(ctx, schema.New(c.bot.DB), c.llmClient, c.bot.DocsConfig, channelID, messageTS, request)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("I've created a documentation update PR: %s", url), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}
