package inbuilt_tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestClient_ListTools(t *testing.T) {
	// Create the client - the key insight is that ListTools doesn't actually
	// execute any tools, it just lists them, so we don't need working database functionality
	client, err := Client(t.Context(), nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Test that ListTools returns a non-nil response
	ctx := context.Background()
	toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.NotNil(t, toolsResult)
	require.NotNil(t, toolsResult.Tools)

	// Verify that at least some tools are available
	require.Greater(t, len(toolsResult.Tools), 0)

	// Verify that the docsearch tool is present with correct properties
	var agentModeToolFound bool
	for _, tool := range toolsResult.Tools {
		if tool.Name == "agent_mode" {
			agentModeToolFound = true
			require.Contains(t, tool.Description, "Enable or disable agent mode")
			require.NotNil(t, tool.InputSchema)
			require.Equal(t, "object", tool.InputSchema.Type)
			require.Contains(t, tool.InputSchema.Properties, "enable")
			require.Contains(t, tool.InputSchema.Required, "enable")
		}
	}
	require.True(t, agentModeToolFound, "agent_mode tool should be available in the tools list")
}
