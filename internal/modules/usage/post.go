package usage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type report struct {
	StartTs      time.Time
	EndTs        time.Time
	ChannelUsage map[string]channelUsage
	ModuleUsage  map[string]moduleUsage
	LLMUsage     map[string]llmUsageStats
	ChannelCount int64
}

type channelUsage struct {
	TotalMessages   int
	TotalThumbsUp   int
	TotalThumbsDown int
}

type moduleUsage struct {
	TotalMessages   int
	TotalThumbsUp   int
	TotalThumbsDown int
}

type llmUsageStats struct {
	TotalRequests     int
	TotalPromptTokens int
	TotalOutputTokens int
}

func get(ctx context.Context, db *schema.Queries, slackIntegration slack_integration.Integration) (report, error) {
	startTs := time.Now().AddDate(0, 0, -7)
	endTs := time.Now()

	channelCount, err := db.CountChannels(ctx)
	if err != nil {
		return report{}, fmt.Errorf("getting channel count: %w", err)
	}

	msgs, err := db.GetMessagesByUser(ctx, schema.GetMessagesByUserParams{
		StartTs: fmt.Sprintf("%d.000000", startTs.Unix()),
		EndTs:   fmt.Sprintf("%d.000000", endTs.Unix()),
		UserID:  slackIntegration.BotUserID(),
	})
	if err != nil {
		return report{}, fmt.Errorf("getting messages: %w", err)
	}

	// Build usage report by channel and module
	channelUsage := make(map[string]channelUsage)
	moduleUsage := make(map[string]moduleUsage)

	for _, msg := range msgs {
		// Channel usage
		usage := channelUsage[msg.ChannelID]
		usage.TotalMessages++

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
		modUsage := moduleUsage[module]
		modUsage.TotalMessages++

		for name, count := range msg.Attrs.Reactions {
			if name == "+1" {
				usage.TotalThumbsUp += count
				modUsage.TotalThumbsUp += count
			} else if name == "-1" {
				usage.TotalThumbsDown += count
				modUsage.TotalThumbsDown += count
			}
		}
		channelUsage[msg.ChannelID] = usage
		moduleUsage[module] = modUsage
	}

	// Get LLM usage data
	llmUsage, err := getLLMUsage(ctx, db, startTs, endTs)
	if err != nil {
		return report{}, fmt.Errorf("getting LLM usage: %w", err)
	}

	return report{
		StartTs:      startTs,
		EndTs:        endTs,
		ChannelUsage: channelUsage,
		ModuleUsage:  moduleUsage,
		LLMUsage:     llmUsage,
		ChannelCount: channelCount,
	}, nil
}

// getLLMUsage fetches LLM usage statistics from the database
func getLLMUsage(ctx context.Context, db *schema.Queries, startTs, endTs time.Time) (map[string]llmUsageStats, error) {
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
	llmUsage := make(map[string]llmUsageStats)
	for _, record := range llmRecords {
		model := record.Model
		stats := llmUsage[model]

		stats.TotalRequests++

		// Add token usage if available
		if record.Output.Usage != nil {
			stats.TotalPromptTokens += record.Output.Usage.PromptTokens
			stats.TotalOutputTokens += record.Output.Usage.CompletionTokens
		}

		llmUsage[model] = stats
	}

	return llmUsage, nil
}

func Post(ctx context.Context, db *schema.Queries, llmClient llm.Client, slackIntegration slack_integration.Integration, channelID string) error {
	report, err := get(ctx, db, slackIntegration)
	if err != nil {
		return fmt.Errorf("getting usage report: %w", err)
	}

	blocks := format(ctx, db, report)
	return slackIntegration.PostMessage(ctx, channelID, blocks...)
}

func format(ctx context.Context, qtx *schema.Queries, report report) []slack.Block {
	type channelStats struct {
		id         string
		messages   int
		thumbsUp   int
		thumbsDown int
	}

	var channels []channelStats
	var totalMessages, totalThumbsUp, totalThumbsDown int
	for id, usage := range report.ChannelUsage {
		totalMessages += usage.TotalMessages
		totalThumbsUp += usage.TotalThumbsUp
		totalThumbsDown += usage.TotalThumbsDown

		channels = append(channels, channelStats{
			id:         id,
			messages:   usage.TotalMessages,
			thumbsUp:   usage.TotalThumbsUp,
			thumbsDown: usage.TotalThumbsDown,
		})
	}

	sort.Slice(channels, func(i, j int) bool {
		return channels[i].messages > channels[j].messages
	})

	type moduleStats struct {
		name       string
		messages   int
		thumbsUp   int
		thumbsDown int
	}

	var modules []moduleStats
	for name, usage := range report.ModuleUsage {
		modules = append(modules, moduleStats{
			name:       name,
			messages:   usage.TotalMessages,
			thumbsUp:   usage.TotalThumbsUp,
			thumbsDown: usage.TotalThumbsDown,
		})
	}

	sort.Slice(modules, func(i, j int) bool {
		return modules[i].messages > modules[j].messages
	})

	// Prepare LLM usage statistics
	type llmStats struct {
		model        string
		requests     int
		promptTokens int
		outputTokens int
	}

	var llmModels []llmStats
	var totalRequests int
	for model, usage := range report.LLMUsage {
		totalRequests += usage.TotalRequests

		llmModels = append(llmModels, llmStats{
			model:        model,
			requests:     usage.TotalRequests,
			promptTokens: usage.TotalPromptTokens,
			outputTokens: usage.TotalOutputTokens,
		})
	}

	sort.Slice(llmModels, func(i, j int) bool {
		return llmModels[i].requests > llmModels[j].requests
	})

	blocks := []slack.Block{
		// Header
		slack.NewHeaderBlock(
			slack.NewTextBlockObject("plain_text", "Ratchet Bot Usage Report", true, false),
		),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("Report period: %s to %s",
					report.StartTs.Format("Jan 2, 2006"),
					report.EndTs.Format("Jan 2, 2006")),
				false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),

		// Summary
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("*Summary*: Channels: %d, Total Messages: %d, Total 👍: %d, Total 👎: %d, LLM Requests: %d",
					report.ChannelCount, totalMessages, totalThumbsUp, totalThumbsDown, totalRequests),
				false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),
	}

	// Add LLM Usage section if we have data
	if len(llmModels) > 0 {
		blocks = append(blocks,
			// LLM Usage Header
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", "*LLM Usage Breakdown*", false, false),
				nil, nil,
			),
		)

		// LLM Usage Table
		var llmTableBuilder strings.Builder
		llmTable := tablewriter.NewTable(&llmTableBuilder,
			tablewriter.WithConfig(tablewriter.Config{
				Header: tw.CellConfig{
					Alignment: tw.CellAlignment{Global: tw.AlignCenter},
				},
				Row: tw.CellConfig{
					Alignment: tw.CellAlignment{Global: tw.AlignLeft},
				},
			}),
		)
		llmTable.Header("Model", "Requests", "Prompt Tokens", "Output Tokens")

		for _, model := range llmModels {
			llmTable.Append([]string{
				model.model,
				fmt.Sprintf("%d", model.requests),
				fmt.Sprintf("%d", model.promptTokens),
				fmt.Sprintf("%d", model.outputTokens),
			})
		}
		llmTable.Render()

		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn",
					"```\n"+llmTableBuilder.String()+"```",
					false, false),
				nil, nil,
			),
			slack.NewDividerBlock(),
		)
	}

	// Module Breakdown Header
	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "*Module Breakdown*", false, false),
			nil, nil,
		),
	)

	// Module Breakdown Table
	var moduleTableBuilder strings.Builder
	moduleTable := tablewriter.NewTable(&moduleTableBuilder,
		tablewriter.WithConfig(tablewriter.Config{
			Header: tw.CellConfig{
				Alignment: tw.CellAlignment{Global: tw.AlignCenter},
			},
			Row: tw.CellConfig{
				Alignment: tw.CellAlignment{Global: tw.AlignLeft},
			},
		}),
	)
	moduleTable.Header("Module", "Messages", "👍", "👎")

	for _, mod := range modules {
		moduleTable.Append([]string{
			mod.name,
			fmt.Sprintf("%d", mod.messages),
			fmt.Sprintf("%d", mod.thumbsUp),
			fmt.Sprintf("%d", mod.thumbsDown),
		})
	}
	moduleTable.Render()

	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				"```\n"+moduleTableBuilder.String()+"```",
				false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),

		// Channel Breakdown Header
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "*Top 5 Channels*", false, false),
			nil, nil,
		),
	)

	// Channel Breakdown Table
	var channelIDs []string
	for i, ch := range channels {
		if i >= 5 {
			break
		}
		channelIDs = append(channelIDs, ch.id)
	}

	channelsByID := make(map[string]schema.ChannelsV2)
	if len(channelIDs) > 0 {
		channels, err := qtx.GetChannels(ctx, channelIDs)
		if err == nil {
			for _, c := range channels {
				channelsByID[c.ID] = c
			}
		}
	}

	var channelTableBuilder strings.Builder
	channelTable := tablewriter.NewTable(&channelTableBuilder,
		tablewriter.WithConfig(tablewriter.Config{
			Header: tw.CellConfig{
				Alignment: tw.CellAlignment{Global: tw.AlignCenter},
			},
			Row: tw.CellConfig{
				Alignment: tw.CellAlignment{Global: tw.AlignLeft},
			},
		}),
	)
	channelTable.Header("Channel", "Messages", "👍", "👎")

	for i, ch := range channels {
		if i >= 5 {
			break
		}

		channelName := ch.id // fallback to ID if not found
		if c, ok := channelsByID[ch.id]; ok {
			channelName = "#" + c.Attrs.Name
		}

		channelTable.Append([]string{
			channelName,
			fmt.Sprintf("%d", ch.messages),
			fmt.Sprintf("%d", ch.thumbsUp),
			fmt.Sprintf("%d", ch.thumbsDown),
		})
	}
	channelTable.Render()

	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				"```\n"+channelTableBuilder.String()+"```",
				false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),
	)

	// Replace old timestamp footer with standardized signature
	blocks = append(blocks, slack_integration.CreateSignatureBlock("Usage Report")...)

	return blocks
}
