package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/openai/openai-go"
)

type MultiServerMCPClient struct {
	clients         map[string]*client.Client
	toolServerNames map[string]string
	allTools        []openai.ChatCompletionToolParam
}

type ServersConfig struct {
	Command string   `json:"command"`
	Env     []string `json:"env"`
	Args    []string `json:"args"`
}

type ServerRegistry struct {
	MCPServers map[string]ServersConfig `json:"mcpServers"`
}

func NewMCPClient(ctx context.Context, mcpFileConfigPath string, serverNames []string) (*MultiServerMCPClient, error) {
	// Parse MCP configurations
	configBytes, err := os.ReadFile(mcpFileConfigPath)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	var config ServerRegistry
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Use specified MCP servers
	mcpClient := MultiServerMCPClient{
		clients:         make(map[string]*client.Client),
		toolServerNames: make(map[string]string),
		allTools:        make([]openai.ChatCompletionToolParam, 0),
	}

	for _, serverName := range serverNames {
		serverConfig, exists := config.MCPServers[serverName]
		if !exists {
			return nil, fmt.Errorf("server %s not found in config", serverName)
		}
		if _, isDuplicate := mcpClient.clients[serverName]; isDuplicate {
			return nil, fmt.Errorf("server added twice: %s", serverName)
		}

		// Communicate via stdio
		c, err := client.NewStdioMCPClient(
			serverConfig.Command, serverConfig.Env, serverConfig.Args...,
		)

		if err != nil {
			return nil, fmt.Errorf("connecting to servers: %w", err)
		}
		c.Initialize(ctx, mcp.InitializeRequest{})
		mcpClient.clients[serverName] = c

		// Gather tool information
		tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})

		if err != nil {
			return nil, fmt.Errorf("failed to get tools from %s: %w", serverName, err)
		}
		for _, tool := range tools.Tools {
			if _, ok := mcpClient.toolServerNames[tool.Name]; ok {
				return nil, fmt.Errorf("duplicate tool name '%s' found when processing server '%s'", tool.Name, serverName)
			}
			mcpClient.toolServerNames[tool.Name] = serverName

			properties := tool.InputSchema.Properties
			if properties == nil {
				properties = make(map[string]interface{})
			}

			required := tool.InputSchema.Required
			if required == nil {
				required = []string{}
			}
			mcpClient.allTools = append(mcpClient.allTools, openai.ChatCompletionToolParam{
				Function: openai.FunctionDefinitionParam{
					Name:        tool.Name,
					Description: openai.String(tool.Description),
					Parameters: openai.FunctionParameters{
						"type":       "object",
						"properties": properties,
						"required":   required,
					},
				},
			})
		}
	}
	return &mcpClient, nil
}

func (msc *MultiServerMCPClient) ListTools(ctx context.Context) ([]openai.ChatCompletionToolParam, error) {
	if msc == nil {
		return nil, nil
	}
	return msc.allTools, nil
}

func (msc *MultiServerMCPClient) CallTools(ctx context.Context, toolCalls []openai.ChatCompletionMessageToolCall) (map[string]string, error) {
	if msc == nil {
		return nil, nil
	}
	resultsByCallID := make(map[string]string)

	for _, toolCall := range toolCalls {
		slog.DebugContext(ctx, "calling tool", "tooCall.Function.Name", toolCall.Function.Name)

		var arguments map[string]interface{}

		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			return nil, fmt.Errorf("parsing arguments for tool call %s: %w", toolCall.Function.Name, err)
		}

		client := msc.clients[msc.toolServerNames[toolCall.Function.Name]]

		result, err := client.CallTool(ctx, mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Name:      toolCall.Function.Name,
				Arguments: arguments,
			},
		})

		if err != nil {
			return nil, fmt.Errorf("tool call failed: %w", err)
		}
		// Only support text based results
		var textParts []string

		for _, content := range result.Content {
			if textContent, ok := mcp.AsTextContent(content); ok {
				textParts = append(textParts, textContent.Text)
			} else {
				return nil, fmt.Errorf("got non-text content from %s", toolCall.Function.Name)
			}
		}
		resultsByCallID[toolCall.ID] = strings.Join(textParts, "\n")
	}
	return resultsByCallID, nil
}

func (msc *MultiServerMCPClient) CloseAll(ctx context.Context) {
	for _, c := range msc.clients {
		c.Close()
	}
}
