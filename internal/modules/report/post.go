package report

import (
	"context"
	"fmt"
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

	incidentCounts := make(map[string]int)                // key: "service/alert"
	incidentDurations := make(map[string][]time.Duration) // key: "service/alert"
	triageMsgCounts := make(map[string]int)               // key: "service/alert"

	for _, msg := range messages {
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

	// Compute frequently firing alerts
	var frequentAlerts []alertEntry
	for alert, count := range incidentCounts {
		if count > 5 {
			service, alertName, _ := strings.Cut(alert, "/")
			frequentAlerts = append(frequentAlerts, alertEntry{
				service: service,
				alert:   alertName,
				count:   count,
			})
		}
	}
	// Sort by count descending and take top 5
	slices.SortFunc(frequentAlerts, func(a, b alertEntry) int {
		return b.count - a.count
	})
	if len(frequentAlerts) > 5 {
		frequentAlerts = frequentAlerts[:5]
	}

	messageBlocks := format(
		channelID,
		incidentCounts,
		incidentDurations,
		suggestions,
		longRunningAlerts,
		untriagedAlerts,
		frequentAlerts,
	)
	return slackIntegration.PostMessage(ctx, channelID, messageBlocks...)
}

func format(
	channelID string,
	incidentCounts map[string]int,
	incidentDurations map[string][]time.Duration,
	suggestions string,
	longRunningAlerts []alertEntry,
	untriagedAlerts []alertEntry,
	frequentAlerts []alertEntry,
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

	// Frequently firing alerts section
	if len(frequentAlerts) > 0 {
		var frequentAlertsTable strings.Builder
		frequentAlertsTable.WriteString("*Frequently Firing Alerts:*\n")
		frequentAlertsTable.WriteString("The following alerts fired more than 5 times this week and may need attention:\n\n```\n")

		table := tablewriter.NewWriter(&frequentAlertsTable)
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

		for _, entry := range frequentAlerts {
			table.Append([]string{
				entry.service,
				entry.alert,
				fmt.Sprintf("%d", entry.count),
			})
		}

		table.Render()
		frequentAlertsTable.WriteString("```\n")
		frequentAlertsTable.WriteString("Consider reviewing these alerts to reduce noise and improve signal.\n")

		messageBlocks = append(messageBlocks,
			slack.NewDividerBlock(),
			slack.NewSectionBlock(
				slack.NewTextBlockObject("mrkdwn", frequentAlertsTable.String(), false, false),
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

	// Replace old timestamp footer with standardized signature
	messageBlocks = append(messageBlocks, slack_integration.CreateSignatureBlock("Weekly Report")...)

	return messageBlocks
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
