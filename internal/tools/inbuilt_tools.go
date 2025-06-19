package tools

import (
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/tools/docrag"
	"github.com/earthboundkid/versioninfo/v2"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/server"
)

// This file contains tool definitions for the commands module.
// Tools are now handled through the simplified JSON-based approach in cmds.go
// rather than complex OpenAI tool calling structures.

// Definitions returns the list of available tools for OpenAI API
func Client(db *schema.Queries, llmClient llm.Client) (*client.Client, error) {
	srv := server.NewMCPServer("ratchet.tools", versioninfo.Short())
	srv.AddTool(docrag.Tool(db, llmClient))

	c, err := client.NewInProcessClient(srv)
	if err != nil {
		return nil, err
	}

	return c, nil
}
