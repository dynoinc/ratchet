package docsearch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pgvector/pgvector-go"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/tools/docutils"
)

type DocSearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type DocSearchResponse struct {
	Query     string                 `json:"query"`
	Documents []DocumentSearchResult `json:"documents"`
}

type DocumentSearchResult struct {
	Identifier string  `json:"identifier"` // Full document URL
	Content    string  `json:"content"`
	Distance   float64 `json:"distance,omitempty"`
}

func Tool(db *schema.Queries, llmClient llm.Client) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name: "docsearch",
		Description: `Find the most relevant documents based on a semantic query.

This tool performs semantic search on the documentation database to find documents
that are most relevant to the given query. It uses embeddings to find semantically
similar content.

Use cases:
- Finding documents for updates: Use limit=1 to get the most relevant document
- Searching for answers: Use limit=10 to get multiple relevant documents for comprehensive answers

The LLM should use this tool to find relevant documentation when a user requests
documentation updates or when trying to understand what documentation might need
to be modified based on conversation context.

IMPORTANT: When rendering an answer based on the search results, always include
the top 3 links from the 'identifier' field of the returned documents. This helps
users verify the information and access the source documentation.

WORKFLOW: This is typically the first step in documentation updates. After finding
relevant documents, use docread to get the full content of the most relevant document.

Returns a list of relevant documents with their content and metadata. Each document
includes an 'identifier' field containing the full document URL.`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"query": map[string]string{
					"type":        "string",
					"description": "Semantic search query to find relevant documents",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of documents to return (use 1 for finding docs to update, 10 for searching answers)",
					"default":     3,
				},
			},
			Required: []string{"query"},
		},
	}

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, err := request.RequireString("query")
		if err != nil {
			return mcp.NewToolResultErrorf("query parameter is required and must be a string: %v", err), nil
		}

		limit := request.GetInt("limit", 3)

		result, err := Execute(ctx, db, llmClient, query, limit)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to search documents", err), nil
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal result", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Execute(ctx context.Context, db *schema.Queries, llmClient llm.Client, query string, limit int) (*DocSearchResponse, error) {
	// Generate embedding for the query
	embedding, err := llmClient.GenerateEmbedding(ctx, "documentation", query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	vec := pgvector.NewVector(embedding)

	// Search for closest documents
	docs, err := db.GetClosestDocs(ctx, schema.GetClosestDocsParams{
		Embedding: &vec,
		LimitVal:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get closest docs: %w", err)
	}

	// Convert to response format
	documents := make([]DocumentSearchResult, 0, len(docs))
	for _, doc := range docs {
		identifier := docutils.MakeURL(doc.Url, doc.Revision, doc.Path)

		documents = append(documents, DocumentSearchResult{
			Identifier: identifier,
			Content:    doc.Content,
		})
	}

	response := &DocSearchResponse{
		Query:     query,
		Documents: documents,
	}

	return response, nil
}
