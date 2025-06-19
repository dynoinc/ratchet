package agent_mode

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type AgentModeRequest struct {
	ChannelID string `json:"channel_id"`
	Enable    bool   `json:"enable"`
}

type AgentModeResponse struct {
	ChannelID string `json:"channel_id"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"`
}

func Tool(db *schema.Queries) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name: "agent_mode",
		Description: `Enable or disable agent mode for a Slack channel.

This tool allows users to control whether Ratchet operates in agent mode for a specific channel.
When agent mode is enabled, Ratchet will proactively monitor and respond to messages in the channel.
When disabled, Ratchet will only respond when explicitly mentioned.

Use cases:
- Enable agent mode: "enable agent mode for this channel"
- Disable agent mode: "disable agent mode for this channel"

The tool requires the channel ID and a boolean flag to enable or disable the mode.`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"channel_id": map[string]string{
					"type":        "string",
					"description": "The Slack channel ID to enable/disable agent mode for",
				},
				"enable": map[string]any{
					"type":        "boolean",
					"description": "True to enable agent mode, false to disable it",
				},
			},
			Required: []string{"channel_id", "enable"},
		},
	}

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channelID, err := request.RequireString("channel_id")
		if err != nil {
			return mcp.NewToolResultErrorf("channel_id parameter is required and must be a string: %v", err), nil
		}

		enable, err := request.RequireBool("enable")
		if err != nil {
			return mcp.NewToolResultErrorf("enable parameter is required and must be a boolean: %v", err), nil
		}

		result, err := Execute(ctx, db, channelID, enable)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to update agent mode", err), nil
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal result", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Execute(ctx context.Context, db *schema.Queries, channelID string, enable bool) (*AgentModeResponse, error) {
	// First, ensure the channel exists
	_, err := db.AddChannel(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure channel exists: %w", err)
	}

	// Update the channel attributes to set agent mode
	err = db.UpdateChannelAttrs(ctx, schema.UpdateChannelAttrsParams{
		ID: channelID,
		Attrs: dto.ChannelAttrs{
			AgentModeEnabled: enable,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update channel attributes: %w", err)
	}

	status := "disabled"
	if enable {
		status = "enabled"
	}

	response := &AgentModeResponse{
		ChannelID: channelID,
		Enabled:   enable,
		Status:    fmt.Sprintf("Agent mode %s successfully", status),
	}

	return response, nil
}
