package upstream_search

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dynoinc/ratchet/internal/docs"
)

type UpstreamSearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type UpstreamSearchResponse struct {
	Query   string                  `json:"query"`
	Results []docs.CodeSearchResult `json:"results"`
}

func Tool(docsConfig *docs.Config) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name: "upstream_search",
		Description: `Search upstream repositories and sources for code snippets to help answer documentation queries.

This tool performs semantic code search across multiple upstream repositories and sources to find relevant code examples,
implementations, or patterns that can help answer user questions about code or documentation.

Use cases:
- Finding code examples for specific functionality or APIs
- Looking up implementation patterns and best practices
- Discovering how other projects solve similar problems
- Finding relevant code snippets for documentation updates
- Searching across multiple upstream sources in parallel for code examples

The tool searches across all configured upstream sources (GitHub repositories, etc.) to find files containing the specified query terms.
Results include repository information, file paths, and code snippets with context from all sources.

IMPORTANT: When presenting search results, always include the repository name and file path
to help users understand the source and context of the code examples.

WORKFLOW: This tool is typically used when answering user questions that require code examples
or when looking for implementation patterns to reference in documentation. For comprehensive answers,
combine this with docsearch to get both code examples and internal documentation.

Use this for questions about "how to implement", "code examples", "API usage", or "implementation patterns".`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"query": map[string]string{
					"type":        "string",
					"description": "Search query for code snippets (e.g., 'function name', 'class definition', 'API endpoint')",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return (default: 5, max: 20)",
					"default":     5,
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

		limit := request.GetInt("limit", 5)
		if limit > 20 {
			limit = 20
		}

		result, err := Execute(ctx, docsConfig, query, limit)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to search upstream sources", err), nil
		}

		jsonData, err := json.Marshal(result)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal result", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Execute(ctx context.Context, docsConfig *docs.Config, query string, limit int) (*UpstreamSearchResponse, error) {
	if docsConfig == nil || len(docsConfig.Sources) == 0 {
		return &UpstreamSearchResponse{
			Query:   query,
			Results: []docs.CodeSearchResult{},
		}, nil
	}

	// Search across all sources in parallel
	var wg sync.WaitGroup
	resultsChan := make(chan []docs.CodeSearchResult, len(docsConfig.Sources))
	errorsChan := make(chan error, len(docsConfig.Sources))

	for _, source := range docsConfig.Sources {
		wg.Add(1)
		go func(s docs.Source) {
			defer wg.Done()

			results, err := s.Search(ctx, query, limit)
			if err != nil {
				errorsChan <- fmt.Errorf("search failed for source %s: %w", s.Name, err)
				return
			}
			resultsChan <- results
		}(source)
	}

	// Wait for all searches to complete
	wg.Wait()
	close(resultsChan)
	close(errorsChan)

	// Check for any errors
	select {
	case err := <-errorsChan:
		return nil, err
	default:
		// No errors, continue
	}

	// Collect all results
	var allResults []docs.CodeSearchResult
	for results := range resultsChan {
		allResults = append(allResults, results...)
	}

	// Limit the total results
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return &UpstreamSearchResponse{
		Query:   query,
		Results: allResults,
	}, nil
}
