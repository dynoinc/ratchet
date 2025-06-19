package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestClient_ListTools(t *testing.T) {
	// Create the client - the key insight is that ListTools doesn't actually
	// execute any tools, it just lists them, so we don't need working database functionality
	client, err := Client(t.Context(), nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Test that ListTools returns a non-nil response
	ctx := context.Background()
	toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.NotNil(t, toolsResult)
	require.NotNil(t, toolsResult.Tools)

	// Verify that at least three tools are available (channel_report, docrag, and usage_report)
	require.Greater(t, len(toolsResult.Tools), 2)

	// Verify that the tools are present with correct properties
	var channelReportToolFound bool
	var docragToolFound bool
	var usageReportToolFound bool
	for _, tool := range toolsResult.Tools {
		if tool.Name == "channel_report" {
			channelReportToolFound = true
			require.Equal(t, "Generate a comprehensive weekly channel report with incident analytics and raw message data.", strings.Split(tool.Description, "\n")[0])
			require.NotNil(t, tool.InputSchema)
			require.Equal(t, "object", tool.InputSchema.Type)
			require.Contains(t, tool.InputSchema.Properties, "channel_id")
			require.Contains(t, tool.InputSchema.Properties, "days")
			require.Contains(t, tool.InputSchema.Required, "channel_id")
		}
		if tool.Name == "docrag" {
			docragToolFound = true
			require.Equal(t, "Search and respond with relevant documentation", tool.Description)
			require.NotNil(t, tool.InputSchema)
			require.Equal(t, "object", tool.InputSchema.Type)
			require.Contains(t, tool.InputSchema.Properties, "query")
			require.Contains(t, tool.InputSchema.Required, "query")
		}
		if tool.Name == "usage_report" {
			usageReportToolFound = true
			require.Equal(t, "Generate usage statistics for Ratchet bot including channel activity, module usage, and LLM consumption.", strings.Split(tool.Description, "\n")[0])
			require.NotNil(t, tool.InputSchema)
			require.Equal(t, "object", tool.InputSchema.Type)
			require.Contains(t, tool.InputSchema.Properties, "days")
		}
	}
	require.True(t, channelReportToolFound, "channel_report tool should be available in the tools list")
	require.True(t, docragToolFound, "docrag tool should be available in the tools list")
	require.True(t, usageReportToolFound, "usage_report tool should be available in the tools list")
}
