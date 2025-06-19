package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/dynoinc/ratchet/internal/tools"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
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

	mcpClients []*client.Client
}

func New(
	ctx context.Context,
	config Config,
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) (*Commands, error) {
	var mcpClients []*client.Client

	// Inbuilt tools
	inbuilt, err := tools.Client(ctx, schema.New(bot.DB), llmClient, slackIntegration)
	if err != nil {
		return nil, fmt.Errorf("creating inbuilt tools client: %w", err)
	}
	mcpClients = append(mcpClients, inbuilt)

	// External MCP servers
	for _, url := range config.MCPServerURLs {
		mc, err := client.NewStdioMCPClient(url, os.Environ())
		if err != nil {
			return nil, fmt.Errorf("creating MCP client: %w", err)
		}

		_, err = mc.Initialize(ctx, mcp.InitializeRequest{})
		if err != nil {
			return nil, fmt.Errorf("initializing MCP client: %w", err)
		}

		slog.DebugContext(ctx, "created MCP client", "url", url)
		mcpClients = append(mcpClients, mc)
	}

	return &Commands{
		config:           config,
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
		mcpClients:       mcpClients,
	}, nil
}

func (c *Commands) Name() string {
	return "commands"
}

func (c *Commands) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	if msg.Message.BotID != "" || msg.Message.BotUsername != "" || msg.Message.SubType != "" {
		return nil
	}

	channel, err := c.bot.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}

	force := channel.Attrs.AgentModeEnabled
	return c.HandleMessage(ctx, channelID, slackTS, msg, force)
}

func (c *Commands) OnThreadMessage(ctx context.Context, channelID string, slackTS string, parentTS string, msg dto.MessageAttrs) error {
	return c.HandleMessage(ctx, channelID, parentTS, msg, false)
}

func (c *Commands) Generate(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs, force bool) (string, error) {
	botID := c.slackIntegration.BotUserID()
	text, found := strings.CutPrefix(msg.Message.Text, fmt.Sprintf("<@%s> ", botID))
	if !found && !force {
		return "", nil
	}

	// Build OpenAI function definitions from the MCP schema
	// (Avoid hand-coding JSON — let MCP do it for you)
	var openAITools []openai.ChatCompletionToolParam
	toolToClient := make(map[string]*client.Client)
	for _, mcpClient := range c.mcpClients {
		tools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
		if err != nil {
			slog.WarnContext(ctx, "listing tools", "error", err)
			continue
		}

		for _, t := range tools.Tools {
			required := t.InputSchema.Required
			if required == nil {
				required = []string{}
			}

			inputSchemaMap := map[string]any{
				"type":       t.InputSchema.Type,
				"properties": t.InputSchema.Properties,
				"required":   required,
			}

			openAITools = append(openAITools, openai.ChatCompletionToolParam{
				Type: "function",
				Function: openai.FunctionDefinitionParam{
					Name:        t.Name,
					Description: openai.String(t.Description),
					Parameters:  openai.FunctionParameters(inputSchemaMap),
				},
			})

			toolToClient[t.Name] = mcpClient
		}
	}

	slog.DebugContext(ctx, "openAITools", "openAITools", openAITools, "toolToClient", toolToClient)

	// Use OpenAI API with tool calling
	params := openai.ChatCompletionNewParams{
		Model: c.llmClient.Model(),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(`You are a helpful assistant that manages Slack channels and provides various utilities.

IMPORTANT INSTRUCTIONS:
1. **ALWAYS use the available tools** when they can help answer the user's request or perform actions
2. **DO NOT** try to answer questions about data, reports, documentation, or channel information without using the appropriate tools first
3. Use multiple tools in parallel when possible to gather comprehensive information
4. If unsure which tools to use, try relevant ones to explore what data is available

RESPONSE FORMAT:
- Format ALL responses in **Markdown** since they will be posted to Slack
- Use proper markdown formatting: headers (##), bullet points (-), code blocks, bold (**text**), etc.
- Structure responses clearly with sections and bullet points
- When you've used tools, briefly mention what you found/did at the beginning

Always be thorough in using tools to provide accurate, up-to-date information rather than making assumptions.`),
			openai.UserMessage(text),
		},
		Tools:             openAITools,
		ParallelToolCalls: openai.Bool(true),
	}

	var response string
	for range 5 {
		openaiClient := c.llmClient.Client()
		completion, err := openaiClient.Chat.Completions.New(ctx, params)
		if err != nil {
			return "", fmt.Errorf("processing request: %w", err)
		}

		toolCalls := completion.Choices[0].Message.ToolCalls
		if len(toolCalls) == 0 {
			response = completion.Choices[0].Message.Content
			break
		}

		params.Messages = append(params.Messages, completion.Choices[0].Message.ToParam())
		for _, toolCall := range toolCalls {
			client, ok := toolToClient[toolCall.Function.Name]
			if !ok {
				slog.ErrorContext(ctx, "Tool not found", "tool", toolCall.Function.Name)
				continue
			}

			var args map[string]any
			if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
				return "", fmt.Errorf("unmarshalling tool call arguments: %w", err)
			}

			res, err := client.CallTool(ctx, mcp.CallToolRequest{
				Params: mcp.CallToolParams{
					Name:      toolCall.Function.Name,
					Arguments: args,
				},
			})
			if err != nil || res.IsError {
				return "", fmt.Errorf("tool %q execution failed: %w", toolCall.Function.Name, err)
			}

			parts := []openai.ChatCompletionContentPartTextParam{}
			for _, content := range res.Content {
				if text, ok := mcp.AsTextContent(content); ok {
					parts = append(parts, openai.ChatCompletionContentPartTextParam{
						Type: "text",
						Text: text.Text,
					})
				}
			}

			params.Messages = append(params.Messages, openai.ChatCompletionMessageParamUnion{
				OfTool: &openai.ChatCompletionToolMessageParam{
					ToolCallID: toolCall.ID,
					Content:    openai.ChatCompletionToolMessageParamContentUnion{OfArrayOfContentParts: parts},
				},
			})
		}
	}

	return response, nil
}

func (c *Commands) HandleMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs, force bool) error {
	response, err := c.Generate(ctx, channelID, slackTS, msg, force)
	if err != nil {
		return err
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
