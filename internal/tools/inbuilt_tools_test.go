package tools

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"
)

func TestClient_ListTools(t *testing.T) {
	// Create the client - the key insight is that ListTools doesn't actually
	// execute any tools, it just lists them, so we don't need working database functionality
	client, err := Client(t.Context(), nil, nil)
	require.NoError(t, err)
	require.NotNil(t, client)

	// Test that ListTools returns a non-nil response
	ctx := context.Background()
	toolsResult, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.NotNil(t, toolsResult)
	require.NotNil(t, toolsResult.Tools)

	// Verify that at least one tool is available (the docrag tool)
	require.Greater(t, len(toolsResult.Tools), 0)

	// Verify that the docrag tool is present with correct properties
	var docragToolFound bool
	for _, tool := range toolsResult.Tools {
		if tool.Name == "docrag" {
			docragToolFound = true
			require.Equal(t, "Search and respond with relevant documentation", tool.Description)
			require.NotNil(t, tool.InputSchema)
			require.Equal(t, "object", tool.InputSchema.Type)
			require.Contains(t, tool.InputSchema.Properties, "query")
			require.Contains(t, tool.InputSchema.Required, "query")
			break
		}
	}
	require.True(t, docragToolFound, "docrag tool should be available in the tools list")
}
