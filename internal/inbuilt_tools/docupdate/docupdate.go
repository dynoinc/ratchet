package docupdate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	docspkg "github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type DocUpdateRequest struct {
	Identifier     string `json:"identifier"`               // Document URL
	UpdatedContent string `json:"updated_content"`          // New content for the document
	CommitMessage  string `json:"commit_message,omitempty"` // Optional commit message
}

type DocUpdateResponse struct {
	Identifier    string `json:"identifier"` // Full document URL
	PRURL         string `json:"pr_url"`
	Status        string `json:"status"`
	CommitMessage string `json:"commit_message,omitempty"`
}

func Tool(db *schema.Queries, docsConfig *docspkg.Config) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name: "DocUpdate",
		Description: `Create a pull request with documentation changes.

This tool creates a pull request with the updated documentation content.
It requires:
- The document identifier (full URL)
- The updated content for the document
- An optional commit message

The tool will:
1. Find the document in the database
2. Create a new branch
3. Update the file with the new content
4. Create a pull request
5. Return the PR URL

WORKFLOW: This is the final step in the documentation update workflow. Only use this
after you have:
1. Found relevant documents with docsearch
2. Reviewed the current content with docread
3. Prepared the updated content
4. Received explicit user approval to proceed

IMPORTANT: Always get user approval before creating pull requests. Show the proposed
changes and ask for confirmation before calling this tool.

This is the final step in the documentation update workflow after the user
has approved the planned changes.`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"identifier": map[string]string{
					"type":        "string",
					"description": "Document URL identifier",
				},
				"updated_content": map[string]string{
					"type":        "string",
					"description": "New content for the document",
				},
				"commit_message": map[string]string{
					"type":        "string",
					"description": "Optional commit message for the PR",
				},
			},
			Required: []string{"identifier", "updated_content"},
		},
	}

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		identifier, err := request.RequireString("identifier")
		if err != nil {
			return mcp.NewToolResultErrorf("identifier parameter is required and must be a string: %v", err), nil
		}

		updatedContent, err := request.RequireString("updated_content")
		if err != nil {
			return mcp.NewToolResultErrorf("updated_content parameter is required and must be a string: %v", err), nil
		}

		commitMessage := request.GetString("commit_message", "Update documentation")

		if docsConfig == nil {
			return mcp.NewToolResultErrorf("documentation config not available"), nil
		}

		result, err := Execute(ctx, db, docsConfig, identifier, updatedContent, commitMessage)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to create PR", err), nil
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal result", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Execute(ctx context.Context, db *schema.Queries, docsConfig *docspkg.Config, identifier, updatedContent, commitMessage string) (*DocUpdateResponse, error) {
	// Parse the identifier to get document parts
	docURL, err := docspkg.ParseURL(identifier)
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

	// Find the matching source
	var source docspkg.Source
	found := false
	for _, s := range docsConfig.Sources {
		if s.URL() == doc.Url {
			source = s
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("no matching source found for document %s", identifier)
	}

	// Create the PR
	prURL, err := source.Suggest(ctx, doc.Path, doc.Revision, updatedContent)
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	response := &DocUpdateResponse{
		Identifier:    identifier,
		PRURL:         prURL,
		Status:        "pull request created successfully",
		CommitMessage: commitMessage,
	}

	return response, nil
}
