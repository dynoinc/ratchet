package docrag

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/pgvector/pgvector-go"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func Tool(db *schema.Queries, llmClient llm.Client) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name:        "docrag",
		Description: "Search and respond with relevant documentation",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"query": map[string]string{
					"type":        "string",
					"description": "The search query or question to look up in documentation",
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

		docs, err := Execute(ctx, db, llmClient, query)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to execute docrag", err), nil
		}

		result := map[string]any{
			"documents": docs,
		}
		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal result", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

type document struct {
	Content string
	Link    string
}

func Execute(ctx context.Context, db *schema.Queries, llmClient llm.Client, query string) ([]document, error) {
	embedding, err := llmClient.GenerateEmbedding(ctx, "query", query)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}
	vec := pgvector.NewVector(embedding)

	docs, err := db.GetClosestDocs(ctx, schema.GetClosestDocsParams{
		Embedding: &vec,
		LimitVal:  5,
	})
	if err != nil {
		return nil, fmt.Errorf("getting closest docs: %w", err)
	}

	contents := make([]document, 0, len(docs))
	for _, doc := range docs {
		contents = append(contents, document{
			Content: doc.Content,
			Link:    fmt.Sprintf("%s/blob/%s/%s", doc.Url, doc.Revision, doc.Path),
		})
	}

	return contents, nil
}
