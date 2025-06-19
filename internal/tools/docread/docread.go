package docread

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/tools/docutils"
)

type DocReadRequest struct {
	Identifier string `json:"identifier"` // Document URL
}

type DocReadResponse struct {
	Identifier string `json:"identifier"` // Full document URL
	Content    string `json:"content"`
}

func Tool(db *schema.Queries) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name: "docread",
		Description: `Read a document by its URL identifier and return the full content.

This tool retrieves a document from the documentation database using its full URL.
The identifier must be a complete document URL (e.g., "https://github.com/owner/repo/blob/main/docs/README.md").

Returns the full document content.`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"identifier": map[string]string{
					"type":        "string",
					"description": "Document URL identifier",
				},
			},
			Required: []string{"identifier"},
		},
	}

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		identifier, err := request.RequireString("identifier")
		if err != nil {
			return mcp.NewToolResultErrorf("identifier parameter is required and must be a string: %v", err), nil
		}

		result, err := Execute(ctx, db, identifier)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to read document", err), nil
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal result", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Execute(ctx context.Context, db *schema.Queries, identifier string) (*DocReadResponse, error) {
	// Parse the identifier to get document parts
	docURL, err := docutils.ParseURL(identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to parse identifier %s: %w", identifier, err)
	}

	// Get the document directly by URL
	doc, err := db.GetDocument(ctx, schema.GetDocumentParams{
		Url:      docURL.BaseURL,
		Path:     docURL.Path,
		Revision: docURL.Revision,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get document by URL: %w", err)
	}

	response := &DocReadResponse{
		Identifier: identifier,
		Content:    doc.Content,
	}

	return response, nil
}
