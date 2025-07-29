package inbuilt_tools

import (
	"context"

	"github.com/earthboundkid/versioninfo/v2"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/inbuilt_tools/channel_report"
	"github.com/dynoinc/ratchet/internal/inbuilt_tools/docread"
	"github.com/dynoinc/ratchet/internal/inbuilt_tools/docsearch"
	"github.com/dynoinc/ratchet/internal/inbuilt_tools/docupdate"
	"github.com/dynoinc/ratchet/internal/inbuilt_tools/usage_report"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

// This file contains tool definitions for the commands module.
// Tools are now handled through the simplified JSON-based approach in cmds.go
// rather than complex OpenAI tool calling structures.

// Definitions returns the list of available tools for OpenAI API
func Client(ctx context.Context, db *schema.Queries, llmClient llm.Client, slackIntegration slack_integration.Integration, docsConfig *docs.Config) (*client.Client, error) {
	srv := server.NewMCPServer("ratchet.tools", versioninfo.Short(), server.WithToolCapabilities(true))
	srv.AddTool(channel_report.Tool(db))
	srv.AddTool(usage_report.Tool(db, slackIntegration))
	if docsConfig != nil {
		srv.AddTool(docread.Tool(db))
		srv.AddTool(docsearch.Tool(db, llmClient))
		srv.AddTool(docupdate.Tool(db, docsConfig))
	}

	c, err := client.NewInProcessClient(srv)
	if err != nil {
		return nil, err
	}

	if err := c.Start(ctx); err != nil {
		return nil, err
	}

	_, err = c.Initialize(ctx, mcp.InitializeRequest{})
	if err != nil {
		return nil, err
	}

	return c, nil
}
