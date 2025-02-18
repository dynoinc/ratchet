package report

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/olekukonko/tablewriter"
	"github.com/slack-go/slack"
)

type alertEntry struct {
	service, alert string
	count          int
	duration       time.Duration
}

func Post(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	channelID string,
) error {
	messages, err := qtx.GetMessagesWithinTS(ctx, schema.GetMessagesWithinTSParams{
		ChannelID: channelID,
		StartTs:   fmt.Sprintf("%d.000000", time.Now().AddDate(0, 0, -7).Unix()),
		EndTs:     fmt.Sprintf("%d.000000", time.Now().Unix()),
	})
	if err != nil {
		return fmt.Errorf("getting messages for channel: %w", err)
	}

	userMsgCounts := make(map[string]int)
	botMsgCounts := make(map[string]int)
	incidentCounts := make(map[string]int)                // key: "service/alert"
	incidentDurations := make(map[string][]time.Duration) // key: "service/alert"
	triageMsgCounts := make(map[string]int)               // key: "service/alert"

	for _, msg := range messages {
		if msg.Attrs.Message.BotID != "" {
			if msg.Attrs.Message.BotUsername != "" {
				botMsgCounts[msg.Attrs.Message.BotUsername]++
			}
		} else {
			userMsgCounts[msg.Attrs.Message.User]++
		}

		incidentKey := fmt.Sprintf("%s/%s", msg.Attrs.IncidentAction.Service, msg.Attrs.IncidentAction.Alert)

		switch msg.Attrs.IncidentAction.Action {
		case dto.ActionOpenIncident:
			incidentCounts[incidentKey]++

			msgs, err := qtx.GetThreadMessagesByServiceAndAlert(ctx, schema.GetThreadMessagesByServiceAndAlertParams{
				Service: msg.Attrs.IncidentAction.Service,
				Alert:   msg.Attrs.IncidentAction.Alert,
			})
			if err != nil {
				return fmt.Errorf("getting thread messages: %w", err)
			}

			triageMsgCounts[incidentKey] += len(msgs)
		case dto.ActionCloseIncident:
			incidentDurations[incidentKey] = append(incidentDurations[incidentKey], msg.Attrs.IncidentAction.Duration.Duration)
		}
	}

	textMessages := make([][]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Attrs.Message.BotID != "" {
			continue
		}

		threadMessages, err := qtx.GetThreadMessages(ctx, schema.GetThreadMessagesParams{
			ChannelID: msg.ChannelID,
			ParentTs:  msg.Ts,
		})
		if err != nil {
			return fmt.Errorf("getting thread messages: %w", err)
		}

		fullThreadMessages := []string{msg.Attrs.Message.Text}
		for _, threadMessage := range threadMessages {
			if threadMessage.Attrs.Message.BotID != "" {
				continue
			}

			fullThreadMessages = append(fullThreadMessages, threadMessage.Attrs.Message.Text)
		}

		textMessages = append(textMessages, fullThreadMessages)
	}

	suggestions, err := llmClient.GenerateChannelSuggestions(ctx, textMessages)
	if err != nil {
		return fmt.Errorf("generating suggestions: %w", err)
	}

	// Compute long-running alerts
	var longRunningAlerts []alertEntry
	for alert, durations := range incidentDurations {
		avgDuration := calculateAverage(durations)
		if avgDuration > 72*time.Hour { // 3 days
			service, alertName, _ := strings.Cut(alert, "/")
			longRunningAlerts = append(longRunningAlerts, alertEntry{
				service:  service,
				alert:    alertName,
				count:    incidentCounts[alert],
				duration: avgDuration,
			})
		}
	}

	// Compute untriaged alerts
	var untriagedAlerts []alertEntry
	for alert, count := range incidentCounts {
		if triageMsgCounts[alert] == 0 {
			service, alertName, _ := strings.Cut(alert, "/")
			untriagedAlerts = append(untriagedAlerts, alertEntry{
				service: service,
				alert:   alertName,
				count:   count,
			})
		}
	}

	messageBlocks := format(channelID, userMsgCounts, botMsgCounts, incidentCounts, incidentDurations, suggestions, longRunningAlerts, untriagedAlerts)
	return slackIntegration.PostMessage(ctx, channelID, messageBlocks...)
}

func format(
	channelID string,
	userMsgCounts, botMsgCounts map[string]int,
	incidentCounts map[string]int,
	incidentDurations map[string][]time.Duration,
	suggestions string,
	longRunningAlerts []alertEntry,
	untriagedAlerts []alertEntry,
) []slack.Block {
	var messageBlocks []slack.Block

	// Build report sections
	headerText := slack.NewTextBlockObject("mrkdwn",
		fmt.Sprintf("*Weekly Channel Report*\nChannel: <#%s>\nPeriod: %s - %s",
			channelID,
			time.Now().AddDate(0, 0, -7).Format("2006-01-02"),
			time.Now().Format("2006-01-02")),
		false, false)
	messageBlocks = append(messageBlocks, slack.NewSectionBlock(headerText, nil, nil))

	// Users section
	if len(userMsgCounts) > 0 {
		var usersList strings.Builder
		for user, count := range sortMapByValue(userMsgCounts, 5) {
			usersList.WriteString(fmt.Sprintf("• <@%s>: %d messages\n", user, count))
		}
		usersText := slack.NewTextBlockObject("mrkdwn",
			fmt.Sprintf("*Top Active Users:*\n%s", usersList.String()),
			false, false)
		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(usersText, nil, nil))
	}

	// Bots section
	if len(botMsgCounts) > 0 {
		var botsList strings.Builder
		for bot, count := range sortMapByValue(botMsgCounts, 5) {
			botsList.WriteString(fmt.Sprintf("• %s: %d messages\n", bot, count))
		}
		botsText := slack.NewTextBlockObject("mrkdwn",
			fmt.Sprintf("*Top Active Bots:*\n%s", botsList.String()),
			false, false)
		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(botsText, nil, nil))
	}

	// Alerts section
	if len(incidentCounts) > 0 {
		var alertsTable strings.Builder
		alertsTable.WriteString("```\n") // Start code block
		table := tablewriter.NewWriter(&alertsTable)
		table.SetHeader([]string{"SERVICE", "ALERT", "COUNT", "AVG DURATION"}) // Shortened headers
		table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
		table.SetCenterSeparator("|")
		table.SetColumnSeparator("|")
		table.SetRowSeparator("-")

		// Disable auto formatting to have more control
		table.SetAutoWrapText(false)
		table.SetAutoFormatHeaders(true)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)

		// Set specific column widths to prevent wrapping
		table.SetColWidth(10) // Minimum width for all columns
		table.SetColumnAlignment([]int{
			tablewriter.ALIGN_LEFT,  // SERVICE
			tablewriter.ALIGN_LEFT,  // ALERT
			tablewriter.ALIGN_RIGHT, // COUNT
			tablewriter.ALIGN_RIGHT, // AVG DURATION
		})

		alerts := make([]alertEntry, 0, len(incidentCounts))
		for alert, count := range incidentCounts {
			service, alertName, _ := strings.Cut(alert, "/")
			avgDuration := calculateAverage(incidentDurations[alert])
			alerts = append(alerts, alertEntry{
				service:  service,
				alert:    alertName,
				count:    count,
				duration: avgDuration,
			})
		}

		// Sort alerts by count (descending), then by service name, then by alert name
		slices.SortFunc(alerts, func(a, b alertEntry) int {
			if a.count != b.count {
				return b.count - a.count // Descending order
			}
			if a.service != b.service {
				return strings.Compare(a.service, b.service)
			}
			return strings.Compare(a.alert, b.alert)
		})

		// Take top 5
		if len(alerts) > 5 {
			alerts = alerts[:5]
		}

		// Add to table with controlled string lengths
		for _, entry := range alerts {
			// Truncate alert name if too long (with ellipsis)
			alertName := entry.alert
			if len(alertName) > 30 {
				alertName = alertName[:27] + "..."
			}

			table.Append([]string{
				entry.service,
				alertName,
				fmt.Sprintf("%d", entry.count),
				entry.duration.Round(time.Second).String(),
			})
		}

		table.Render()
		alertsTable.WriteString("```\n") // Add newline after closing code block

		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", "*Top Alerts:*", false, false),
				nil, nil),
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", alertsTable.String(), false, false),
				nil, nil))
	}

	// Long-running alerts section
	if len(longRunningAlerts) > 0 {
		var longAlertsTable strings.Builder
		longAlertsTable.WriteString("*Alerts with Long Resolution Times:*\n")
		longAlertsTable.WriteString("The following alerts consistently take more than 3 days to resolve and may need review:\n\n```\n")

		table := tablewriter.NewWriter(&longAlertsTable)
		table.SetHeader([]string{"SERVICE", "ALERT", "AVG DURATION"})
		table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
		table.SetCenterSeparator("|")
		table.SetColumnSeparator("|")
		table.SetRowSeparator("-")
		table.SetAutoWrapText(false)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetColumnAlignment([]int{
			tablewriter.ALIGN_LEFT,  // SERVICE
			tablewriter.ALIGN_LEFT,  // ALERT
			tablewriter.ALIGN_RIGHT, // AVG DURATION
		})

		for _, entry := range longRunningAlerts {
			table.Append([]string{
				entry.service,
				entry.alert,
				entry.duration.Round(time.Hour).String(),
			})
		}

		table.Render()
		longAlertsTable.WriteString("```\n")
		longAlertsTable.WriteString("Consider reviewing and potentially removing these alerts if they're not providing value.\n")

		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", longAlertsTable.String(), false, false),
				nil, nil))
	}

	// Untriaged alerts section
	if len(untriagedAlerts) > 0 {
		var untriagedAlertsTable strings.Builder
		untriagedAlertsTable.WriteString("*Alerts with No Triage Activity:*\n")
		untriagedAlertsTable.WriteString("The following alerts had no team interaction and may be unnecessary:\n\n```\n")

		table := tablewriter.NewWriter(&untriagedAlertsTable)
		table.SetHeader([]string{"SERVICE", "ALERT", "COUNT"})
		table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
		table.SetCenterSeparator("|")
		table.SetColumnSeparator("|")
		table.SetRowSeparator("-")
		table.SetAutoWrapText(false)
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
		table.SetAlignment(tablewriter.ALIGN_LEFT)
		table.SetColumnAlignment([]int{
			tablewriter.ALIGN_LEFT,  // SERVICE
			tablewriter.ALIGN_LEFT,  // ALERT
			tablewriter.ALIGN_RIGHT, // COUNT
		})

		for _, entry := range untriagedAlerts {
			table.Append([]string{
				entry.service,
				entry.alert,
				fmt.Sprintf("%d", entry.count),
			})
		}

		table.Render()
		untriagedAlertsTable.WriteString("```\n")
		untriagedAlertsTable.WriteString("Consider removing these alerts as they don't seem to require team attention.\n")

		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", untriagedAlertsTable.String(), false, false),
				nil, nil))
	}

	// Format suggestions
	if len(suggestions) > 0 {
		suggestionLines := strings.Split(suggestions, "\n")
		var formattedSuggestions strings.Builder
		formattedSuggestions.WriteString("*Suggestions for Improvement:*\n\n")

		for _, suggestion := range suggestionLines {
			suggestion = strings.TrimSpace(suggestion)
			if suggestion == "" {
				continue
			}
			if !strings.HasPrefix(suggestion, "•") && !strings.HasPrefix(suggestion, "*") && !strings.HasPrefix(suggestion, "-") {
				formattedSuggestions.WriteString("• ")
			}
			formattedSuggestions.WriteString(suggestion)
			formattedSuggestions.WriteString("\n")
		}

		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", formattedSuggestions.String(), false, false),
				nil, nil))
	}

	// Add signature
	messageBlocks = append(messageBlocks,
		slack.NewDividerBlock(),
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn",
				fmt.Sprintf("_Generated by Ratchet Bot at %s_",
					time.Now().Format("2006-01-02 15:04:05 MST")),
				false, false),
			nil, nil))

	return messageBlocks
}

type kv struct {
	k string
	v int
}

// sortMapByValue sorts a map by value in descending order and returns the top i entries.
func sortMapByValue(counts map[string]int, i int) iter.Seq2[string, int] {
	entries := make([]kv, 0, len(counts))
	for k, v := range counts {
		entries = append(entries, kv{k: k, v: v})
	}
	// Sort by count (descending) first, then by key for stable ordering
	slices.SortFunc(entries, func(a, b kv) int {
		if a.v != b.v {
			return b.v - a.v // Descending order
		}
		return strings.Compare(a.k, b.k) // Alphabetical by key if counts are equal
	})
	if i > len(entries) {
		i = len(entries)
	}
	return func(yield func(string, int) bool) {
		for _, entry := range entries[:i] {
			if !yield(entry.k, entry.v) {
				return
			}
		}
	}
}

func calculateAverage(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	total := time.Duration(0)
	for _, duration := range durations {
		total += duration
	}

	return total / time.Duration(len(durations))
}
