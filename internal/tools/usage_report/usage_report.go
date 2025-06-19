package usage_report

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type UsageReport struct {
	StartDate    string         `json:"start_date"`
	EndDate      string         `json:"end_date"`
	ChannelCount int64          `json:"channel_count"`
	Summary      Summary        `json:"summary"`
	Channels     []ChannelUsage `json:"channels"`
	Modules      []ModuleUsage  `json:"modules"`
	LLMUsage     []LLMUsage     `json:"llm_usage"`
}

type Summary struct {
	TotalMessages    int `json:"total_messages"`
	TotalThumbsUp    int `json:"total_thumbs_up"`
	TotalThumbsDown  int `json:"total_thumbs_down"`
	TotalLLMRequests int `json:"total_llm_requests"`
}

type ChannelUsage struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Messages   int    `json:"messages"`
	ThumbsUp   int    `json:"thumbs_up"`
	ThumbsDown int    `json:"thumbs_down"`
}

type ModuleUsage struct {
	Name       string `json:"name"`
	Messages   int    `json:"messages"`
	ThumbsUp   int    `json:"thumbs_up"`
	ThumbsDown int    `json:"thumbs_down"`
}

type LLMUsage struct {
	Model        string `json:"model"`
	Requests     int    `json:"requests"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

func Tool(db *schema.Queries, slackIntegration slack_integration.Integration) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name:        "usage_report",
		Description: "Generate usage statistics for Ratchet bot including channel activity, module usage, and LLM consumption",
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"days": map[string]any{
					"type":        "integer",
					"description": "Number of days to look back for the report (default: 7)",
					"default":     7,
				},
			},
		},
	}

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		days := request.GetInt("days", 7)

		report, err := Generate(ctx, db, slackIntegration, days)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to generate usage report", err), nil
		}

		jsonData, err := json.Marshal(report)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal report", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Generate(ctx context.Context, db *schema.Queries, slackIntegration slack_integration.Integration, days int) (*UsageReport, error) {
	startTs := time.Now().AddDate(0, 0, -days)
	endTs := time.Now()

	channelCount, err := db.CountChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting channel count: %w", err)
	}

	msgs, err := db.GetMessagesByUser(ctx, schema.GetMessagesByUserParams{
		StartTs: fmt.Sprintf("%d.000000", startTs.Unix()),
		EndTs:   fmt.Sprintf("%d.000000", endTs.Unix()),
		UserID:  slackIntegration.BotUserID(),
	})
	if err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}

	// Build usage data
	channelUsage := make(map[string]*ChannelUsage)
	moduleUsage := make(map[string]*ModuleUsage)

	for _, msg := range msgs {
		// Channel usage
		if _, exists := channelUsage[msg.ChannelID]; !exists {
			channelUsage[msg.ChannelID] = &ChannelUsage{
				ID: msg.ChannelID,
			}
		}
		channelUsage[msg.ChannelID].Messages++

		// Module usage
		module := "unknown"
		if msg.Attrs.Message.Text != "" {
			// Look for module name in signature block
			if strings.Contains(msg.Attrs.Message.Text, "[module:") {
				parts := strings.Split(msg.Attrs.Message.Text, "[module:")
				if len(parts) > 1 {
					module = strings.Split(parts[1], "]")[0]
				}
			}
		}

		if _, exists := moduleUsage[module]; !exists {
			moduleUsage[module] = &ModuleUsage{
				Name: module,
			}
		}
		moduleUsage[module].Messages++

		// Process reactions
		for name, count := range msg.Attrs.Reactions {
			switch name {
			case "+1":
				channelUsage[msg.ChannelID].ThumbsUp += count
				moduleUsage[module].ThumbsUp += count
			case "-1":
				channelUsage[msg.ChannelID].ThumbsDown += count
				moduleUsage[module].ThumbsDown += count
			}
		}
	}

	// Get channel names
	var channelIDs []string
	for id := range channelUsage {
		channelIDs = append(channelIDs, id)
	}

	if len(channelIDs) > 0 {
		channels, err := db.GetChannels(ctx, channelIDs)
		if err == nil {
			for _, c := range channels {
				if usage, exists := channelUsage[c.ID]; exists {
					usage.Name = c.Attrs.Name
				}
			}
		}
	}

	// Get LLM usage data
	llmUsage, err := getLLMUsage(ctx, db, startTs, endTs)
	if err != nil {
		return nil, fmt.Errorf("getting LLM usage: %w", err)
	}

	// Convert maps to slices and calculate summary
	var channels []ChannelUsage
	var modules []ModuleUsage
	var summary Summary

	for _, usage := range channelUsage {
		channels = append(channels, *usage)
		summary.TotalMessages += usage.Messages
		summary.TotalThumbsUp += usage.ThumbsUp
		summary.TotalThumbsDown += usage.ThumbsDown
	}

	for _, usage := range moduleUsage {
		modules = append(modules, *usage)
	}

	for _, usage := range llmUsage {
		summary.TotalLLMRequests += usage.Requests
	}

	return &UsageReport{
		StartDate:    startTs.Format("2006-01-02"),
		EndDate:      endTs.Format("2006-01-02"),
		ChannelCount: channelCount,
		Summary:      summary,
		Channels:     channels,
		Modules:      modules,
		LLMUsage:     llmUsage,
	}, nil
}

func getLLMUsage(ctx context.Context, db *schema.Queries, startTs, endTs time.Time) ([]LLMUsage, error) {
	// Convert timestamps to pgtype.Timestamptz
	start := pgtype.Timestamptz{}
	if err := start.Scan(startTs); err != nil {
		return nil, fmt.Errorf("scanning start timestamp: %w", err)
	}

	end := pgtype.Timestamptz{}
	if err := end.Scan(endTs); err != nil {
		return nil, fmt.Errorf("scanning end timestamp: %w", err)
	}

	// Fetch LLM usage records for the given time range
	llmRecords, err := db.GetLLMUsageByTimeRange(ctx, schema.GetLLMUsageByTimeRangeParams{
		StartTime: start,
		EndTime:   end,
	})
	if err != nil {
		return nil, fmt.Errorf("querying LLM usage: %w", err)
	}

	// Aggregate usage statistics by model
	llmUsageMap := make(map[string]*LLMUsage)
	for _, record := range llmRecords {
		model := record.Model
		if _, exists := llmUsageMap[model]; !exists {
			llmUsageMap[model] = &LLMUsage{
				Model: model,
			}
		}

		llmUsageMap[model].Requests++

		// Add token usage if available
		if record.Output.Usage != nil {
			llmUsageMap[model].PromptTokens += record.Output.Usage.PromptTokens
			llmUsageMap[model].OutputTokens += record.Output.Usage.CompletionTokens
		}
	}

	// Convert map to slice
	var result []LLMUsage
	for _, usage := range llmUsageMap {
		result = append(result, *usage)
	}

	return result, nil
}
