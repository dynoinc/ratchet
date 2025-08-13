package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openai/openai-go"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.22.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/inbuilt_tools"
	"github.com/dynoinc/ratchet/internal/llm"
	rsemconv "github.com/dynoinc/ratchet/internal/otel/semconv"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	MCPServerURLs []string `envconfig:"MCP_SERVER_URLS"`
}

type Commands struct {
	config           Config
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
	tracer           trace.Tracer
	mcpClients       []*client.Client
}

func New(
	ctx context.Context,
	config Config,
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
	docsConfig *docs.Config,
) (*Commands, error) {
	var mcpClients []*client.Client

	// Inbuilt tools
	inbuilt, err := inbuilt_tools.Client(ctx, schema.New(bot.DB), llmClient, slackIntegration, docsConfig)
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
		tracer:           otel.Tracer("ratchet.commands"),
	}, nil
}

func (c *Commands) Name() string {
	return "commands"
}

// startSlackSpan creates a span with standardized Slack attributes
func (c *Commands) startSlackSpan(ctx context.Context, name, channelID, userID, slackTS string) (context.Context, trace.Span) {
	return c.tracer.Start(ctx, name,
		trace.WithAttributes(
			rsemconv.SlackUserKey.String(userID),
			rsemconv.SlackChannelIDKey.String(channelID),
			rsemconv.SlackTimestampKey.String(slackTS),
			rsemconv.ForceTraceKey.Bool(true),
		),
	)
}

// startToolSpan creates a span for tool execution with standardized attributes
func (c *Commands) startToolSpan(ctx context.Context, tool mcp.Tool, toolCall openai.ChatCompletionMessageToolCall) (context.Context, trace.Span) {
	parentSpan := trace.SpanFromContext(ctx)
	if parentSpan == nil || !parentSpan.SpanContext().IsValid() {
		return ctx, trace.SpanFromContext(context.Background())
	}

	spanName := fmt.Sprintf("%s %s", llm.OperationExecuteTool, tool.Name)
	attrs := []attribute.KeyValue{
		llm.GenAIOperationNameKey.String(string(llm.OperationExecuteTool)),
		llm.GenAIToolNameKey.String(tool.Name),
		llm.GenAIToolCallIDKey.String(toolCall.ID),
	}
	if tool.Description != "" {
		attrs = append(attrs, llm.GenAIToolDescriptionKey.String(tool.Description))
	}

	return c.tracer.Start(ctx, spanName,
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindInternal),
	)
}

func (c *Commands) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	if msg.Message.BotID != "" || msg.Message.BotUsername != "" || msg.Message.SubType != "" {
		return nil
	}

	return c.Respond(ctx, channelID, slackTS, msg)
}

func (c *Commands) OnThreadMessage(ctx context.Context, channelID string, slackTS string, parentTS string, msg dto.MessageAttrs) error {
	return c.Respond(ctx, channelID, parentTS, msg)
}

func (c *Commands) Generate(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) (string, error) {
	ctx, span := c.startSlackSpan(ctx, "commands.generate", channelID, msg.Message.User, slackTS)
	defer span.End()
	botID := c.slackIntegration.BotUserID()
	if !strings.HasPrefix(msg.Message.Text, fmt.Sprintf("<@%s> ", botID)) {
		return "", nil
	}

	topMsg, err := c.bot.GetMessage(ctx, channelID, slackTS)
	if err != nil {
		return "", fmt.Errorf("getting top message: %w", err)
	}

	// Get thread messages for context
	threadMessages, err := c.getThreadMessages(ctx, channelID, slackTS)
	if err != nil {
		slog.WarnContext(ctx, "failed to get thread messages for context", "error", err)
	}

	var openAITools []openai.ChatCompletionToolParam
	toolToClient := make(map[string]*client.Client)
	toolByName := make(map[string]mcp.Tool)
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

			properties := t.InputSchema.Properties
			if properties == nil {
				properties = map[string]any{}
			}

			inputSchemaMap := map[string]any{
				"type":       t.InputSchema.Type,
				"properties": properties,
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
			toolByName[t.Name] = t
		}
	}

	// Build conversation history from thread messages
	var conversationHistory []openai.ChatCompletionMessageParamUnion

	// Build system message with context
	systemPrompt := c.GetSystemPrompt(ctx, channelID, topMsg.Attrs.IncidentAction)
	conversationHistory = append(conversationHistory, openai.SystemMessage(systemPrompt))

	// Add the top message
	topMsgText := strings.TrimPrefix(topMsg.Attrs.Message.Text, fmt.Sprintf("<@%s> ", botID))
	timestamp := slackTsToRFC3339(ctx, topMsg.Ts)
	msgWithTimestamp := fmt.Sprintf("[%s] %s", timestamp, topMsgText)
	conversationHistory = append(conversationHistory, openai.UserMessage(msgWithTimestamp))

	// Add thread history
	for _, threadMsg := range threadMessages {
		if threadMsg.Attrs.Message.User == c.slackIntegration.BotUserID() {
			// Assistant message
			timestamp := slackTsToRFC3339(ctx, threadMsg.Ts)
			msgWithTimestamp := fmt.Sprintf("[%s] %s", timestamp, threadMsg.Attrs.Message.Text)
			conversationHistory = append(conversationHistory, openai.AssistantMessage(msgWithTimestamp))
		} else {
			// User message
			threadMsgText := strings.TrimPrefix(threadMsg.Attrs.Message.Text, fmt.Sprintf("<@%s> ", c.slackIntegration.BotUserID()))
			timestamp := slackTsToRFC3339(ctx, threadMsg.Ts)
			msgWithTimestamp := fmt.Sprintf("[%s] %s", timestamp, threadMsgText)
			conversationHistory = append(conversationHistory, openai.UserMessage(msgWithTimestamp))
		}
	}

	var response string
	for range 5 {
		completion, err := c.llmClient.RunChatCompletionWithTools(ctx, conversationHistory, openAITools)
		if err != nil {
			return "", fmt.Errorf("processing request: %w", err)
		}

		toolCalls := completion.Choices[0].Message.ToolCalls
		if len(toolCalls) == 0 {
			response = completion.Choices[0].Message.Content
			break
		}

		conversationHistory = append(conversationHistory, completion.Choices[0].Message.ToParam())
		for _, toolCall := range toolCalls {
			client, ok := toolToClient[toolCall.Function.Name]
			if !ok {
				slog.ErrorContext(ctx, "Tool not found", "tool", toolCall.Function.Name)
				continue
			}
			tool, ok := toolByName[toolCall.Function.Name]
			if !ok {
				slog.ErrorContext(ctx, "Tool not found", "tool", toolCall.Function.Name)
				continue
			}

			slog.DebugContext(ctx, "calling tool", "tool", toolCall.Function.Name, "id", toolCall.ID)
			res, err := c.callTool(ctx, client, tool, toolCall)
			if err != nil {
				return "", fmt.Errorf("tool %q execution failed: %w", toolCall.Function.Name, err)
			}
			slog.DebugContext(ctx, "tool call result", "tool", toolCall.Function.Name, "id", toolCall.ID, "error", err)

			if res.IsError {
				jsn, _ := json.Marshal(res)
				return "", fmt.Errorf("tool %q execution failed: %s", toolCall.Function.Name, string(jsn))
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

			conversationHistory = append(conversationHistory, openai.ChatCompletionMessageParamUnion{
				OfTool: &openai.ChatCompletionToolMessageParam{
					ToolCallID: toolCall.ID,
					Content:    openai.ChatCompletionToolMessageParamContentUnion{OfArrayOfContentParts: parts},
				},
			})
		}
	}

	span.SetAttributes(rsemconv.ResponseMessageSizeKey.Int(len(response)))
	if response == "" {
		span.SetStatus(codes.Error, "empty response")
	}

	return response, nil
}

// getThreadMessages retrieves the last 10 messages in a thread
func (c *Commands) getThreadMessages(ctx context.Context, channelID string, slackTS string) ([]schema.GetThreadMessagesRow, error) {
	// Get thread messages from database
	threadMessages, err := schema.New(c.bot.DB).GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: channelID,
		ParentTs:  slackTS,
		BotID:     "", // Don't filter out bot messages, we'll classify them ourselves
		LimitVal:  10,
	})
	if err != nil {
		return nil, fmt.Errorf("getting thread messages: %w", err)
	}

	return threadMessages, nil
}

func (c *Commands) Respond(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	ctx, span := c.startSlackSpan(ctx, "commands.respond", channelID, msg.Message.User, slackTS)
	defer span.End()
	response, err := c.Generate(ctx, channelID, slackTS, msg)
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

// callTool wraps client.CallTool with OpenTelemetry tracing
func (c *Commands) callTool(ctx context.Context, client *client.Client, tool mcp.Tool, toolCall openai.ChatCompletionMessageToolCall) (*mcp.CallToolResult, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil, fmt.Errorf("unmarshalling tool call arguments: %w", err)
	}

	ctx, span := c.startToolSpan(ctx, tool, toolCall)
	defer span.End()

	res, err := client.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolCall.Function.Name,
			Arguments: args,
		},
	})

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(semconv.ErrorTypeKey.String(fmt.Sprintf("%T", err)))
		return nil, err
	}

	if res.IsError {
		span.SetStatus(codes.Error, "tool execution returned error")
		span.SetAttributes(semconv.ErrorTypeKey.String("tool_execution_error"))
	} else {
		span.SetStatus(codes.Ok, "tool execution successful")
	}

	return res, nil
}

// slackTsToRFC3339 converts a Slack timestamp to RFC3339 format or returns original if conversion fails
func slackTsToRFC3339(ctx context.Context, slackTs string) string {
	// Parse Slack timestamp (format: "1355517523.000005")
	parts := strings.Split(slackTs, ".")
	if len(parts) != 2 {
		slog.WarnContext(ctx, "unable to parse slack timestamp: invalid format", "ts", slackTs)
		return slackTs
	}

	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		slog.WarnContext(ctx, "unable to parse slack timestamp: invalid seconds", "ts", slackTs, "error", err)
		return slackTs
	}

	microseconds, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		slog.WarnContext(ctx, "unable to parse slack timestamp: invalid microseconds", "ts", slackTs, "error", err)
		return slackTs
	}

	t := time.Unix(seconds, microseconds*1000)
	return t.UTC().Format(time.RFC3339)
}

// GetSystemPrompt builds the complete system prompt with context and MCP instructions
func (c *Commands) GetSystemPrompt(ctx context.Context, channelID string, incidentAction dto.IncidentAction) string {
	systemPrompt := fmt.Sprintf(`You are a helpful assistant that manages Slack channels and provides various utilities.

CURRENT CONTEXT:
- Channel ID: %s
- Use this channel_id when tools require it`, channelID)

	// Add alert firing context if an incident is open
	if incidentAction.Action == dto.ActionOpenIncident {
		systemPrompt += fmt.Sprintf(`
- Alert Context: Service "%s" - Alert "%s" (Priority: %s)`,
			incidentAction.Service,
			incidentAction.Alert,
			incidentAction.Priority)
	}

	systemPrompt += `

IMPORTANT INSTRUCTIONS:
1. **PRIMARY FOCUS**: Always prioritize and focus on the latest user request. Use conversation history primarily for context and understanding, not as the main topic.
2. **ALWAYS use the available tools** when they can help answer the user's request or perform actions
3. **DO NOT** try to answer questions about data, reports, documentation, or channel information without using the appropriate tools first
4. If unsure which tools to use, try relevant ones to explore what data is available
5. **ONLY offer capabilities that are available through your tools** - do not suggest checking external systems like GitHub status, deployment status, or other services unless you have specific tools for them
6. If a user asks about something you cannot do with available tools, politely explain what you can help with instead
7. **RESPOND TO THE CURRENT REQUEST**: Even if the conversation history contains previous topics or requests, always address the most recent user message first

DOCUMENTATION SEARCH STRATEGY:
When answering documentation questions, use BOTH search tools strategically:

**For Internal Documentation Questions:**
- Use docsearch to find relevant internal documentation from the database
- This searches through existing documentation that has been indexed
- Use limit=10 for comprehensive answers, limit=1 for finding specific docs to update

**For Comprehensive Documentation Answers:**
- Use docsearch to find relevant internal documentation
- This searches through existing documentation that has been indexed

DOCUMENTATION UPDATE WORKFLOW:
When a user requests documentation updates, follow this 2-step process:
1. **STEP 1 - Find and Review**: Use docsearch to find relevant documents, then use docread to get the full content of the most relevant document
2. **STEP 2 - Update**: After reviewing the current content, use docupdate to create a pull request with the proposed changes
Always explain what you're doing at each step and get user approval before proceeding to step 2.`

	// Add MCP server instructions if available
	for _, mcpClient := range c.mcpClients {
		// Try to get the "instructions" prompt and skip silently if not found
		promptResult, err := mcpClient.GetPrompt(ctx, mcp.GetPromptRequest{
			Params: mcp.GetPromptParams{
				Name: "instructions",
			},
		})
		if err != nil {
			continue
		}
		for _, msg := range promptResult.Messages {
			if textContent, ok := mcp.AsTextContent(msg.Content); ok {
				systemPrompt += "\n\n---\n\n" + textContent.Text
			}
		}
	}
	systemPrompt += `

---

RESPONSE FORMAT:
You are writing for a Slack section block. Use Slack's mrkdwn format:
• *bold*, _italic_, ~strike~
• Bullet lists with * or - 
• Inline code and code blocks
• Blockquotes with >
• Present all links as <url|text> (e.g. <https://www.google.com/|Google>). **Do not** display the raw link directly. 

Do NOT use: headings (#), tables, or HTML.
Keep responses under 3000 characters.

Always be thorough in using tools to provide accurate, up-to-date information rather than making assumptions.
`

	return systemPrompt
}
