package usage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/olekukonko/tablewriter"
	"github.com/slack-go/slack"
)

type UsageReport struct {
	StartTs      time.Time
	EndTs        time.Time
	ChannelUsage map[string]ChannelUsage
	ModuleUsage  map[string]ModuleUsage
}

type ChannelUsage struct {
	TotalMessages   int
	TotalThumbsUp   int
	TotalThumbsDown int
}

type ModuleUsage struct {
	TotalMessages   int
	TotalThumbsUp   int
	TotalThumbsDown int
}

func Get(ctx context.Context, db *schema.Queries, llmClient llm.Client, slackIntegration slack_integration.Integration, channelID string) (UsageReport, error) {
	startTs := time.Now().AddDate(0, 0, -7)
	endTs := time.Now()

	msgs, err := db.GetMessagesByUser(ctx, schema.GetMessagesByUserParams{
		StartTs: fmt.Sprintf("%d.000000", startTs.Unix()),
		EndTs:   fmt.Sprintf("%d.000000", endTs.Unix()),
		UserID:  slackIntegration.BotUserID(),
	})
	if err != nil {
		return UsageReport{}, fmt.Errorf("getting messages: %w", err)
	}

	// Build usage report by channel and module
	channelUsage := make(map[string]ChannelUsage)
	moduleUsage := make(map[string]ModuleUsage)

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

	return UsageReport{
		StartTs:      startTs,
		EndTs:        endTs,
		ChannelUsage: channelUsage,
		ModuleUsage:  moduleUsage,
	}, nil
}

func Post(ctx context.Context, db *schema.Queries, llmClient llm.Client, slackIntegration slack_integration.Integration, channelID string) error {
	report, err := Get(ctx, db, llmClient, slackIntegration, channelID)
	if err != nil {
		return fmt.Errorf("getting usage report: %w", err)
	}

	blocks := Format(ctx, db, slackIntegration, channelID, report)
	return slackIntegration.PostMessage(ctx, channelID, blocks...)
}

func Format(ctx context.Context, qtx *schema.Queries, slackIntegration slack_integration.Integration, channelID string, report UsageReport) []slack.Block {
	// Calculate totals
	var totalMessages, totalThumbsUp, totalThumbsDown int
	for _, usage := range report.ChannelUsage {
		totalMessages += usage.TotalMessages
		totalThumbsUp += usage.TotalThumbsUp
		totalThumbsDown += usage.TotalThumbsDown
	}

	// Sort channels by message count
	type channelStats struct {
		id         string
		messages   int
		thumbsUp   int
		thumbsDown int
	}

	var channels []channelStats
	for id, usage := range report.ChannelUsage {
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

	// Sort modules by message count
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
				fmt.Sprintf("*Summary*: Total Messages: %d, Total 👍: %d, Total 👎: %d",
					totalMessages, totalThumbsUp, totalThumbsDown),
				false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),

		// Module Breakdown Header
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "*Module Breakdown*", false, false),
			nil, nil,
		),
	}

	// Module Breakdown Table
	var moduleTableBuilder strings.Builder
	moduleTable := tablewriter.NewWriter(&moduleTableBuilder)
	moduleTable.SetHeader([]string{"Module", "Messages", "👍", "👎"})
	moduleTable.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
	moduleTable.SetCenterSeparator("|")
	moduleTable.SetAlignment(tablewriter.ALIGN_LEFT)

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
	channelTable := tablewriter.NewWriter(&channelTableBuilder)
	channelTable.SetHeader([]string{"Channel", "Messages", "👍", "👎"})
	channelTable.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
	channelTable.SetCenterSeparator("|")
	channelTable.SetAlignment(tablewriter.ALIGN_LEFT)

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
