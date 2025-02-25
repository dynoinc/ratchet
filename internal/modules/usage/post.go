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
}

type ChannelUsage struct {
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

	// Build usage report by channel
	channelUsage := make(map[string]ChannelUsage)
	for _, msg := range msgs {
		usage := channelUsage[msg.ChannelID]
		usage.TotalMessages++

		for name, count := range msg.Attrs.Reactions {
			if name == "+1" {
				usage.TotalThumbsUp += count
			} else if name == "-1" {
				usage.TotalThumbsDown += count
			}
		}
		channelUsage[msg.ChannelID] = usage
	}

	return UsageReport{
		StartTs:      startTs,
		EndTs:        endTs,
		ChannelUsage: channelUsage,
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

		// Channel Breakdown Header
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "*Top 5 Channels*", false, false),
			nil, nil,
		),
	}

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

	// Create table writer with string builder
	var tableBuilder strings.Builder
	table := tablewriter.NewWriter(&tableBuilder)
	table.SetHeader([]string{"Channel", "Messages", "👍", "👎"})
	table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
	table.SetCenterSeparator("|")
	table.SetAlignment(tablewriter.ALIGN_LEFT)

	for i, ch := range channels {
		if i >= 5 {
			break
		}

		channelName := ch.id // fallback to ID if not found
		if c, ok := channelsByID[ch.id]; ok {
			channelName = "#" + c.Attrs.Name
		}

		table.Append([]string{
			channelName,
			fmt.Sprintf("%d", ch.messages),
			fmt.Sprintf("%d", ch.thumbsUp),
			fmt.Sprintf("%d", ch.thumbsDown),
		})
	}
	table.Render()

	blocks = append(blocks,
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				"```\n"+tableBuilder.String()+"```",
				false, false),
			nil, nil,
		),
		slack.NewDividerBlock(),

		// Footer with timestamp
		slack.NewContextBlock("",
			[]slack.MixedElement{
				slack.NewTextBlockObject("mrkdwn",
					fmt.Sprintf("_Generated by Ratchet Bot at %s_",
						time.Now().Format("2006-01-02 15:04:05 MST")),
					false, false),
			}...),
	)

	return blocks
}
