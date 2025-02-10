package report_worker

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/olekukonko/tablewriter"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"
)

type reportWorker struct {
	river.WorkerDefaults[background.ReportWorkerArgs]

	bot              *internal.Bot
	slackIntegration *slack_integration.Integration
	llmClient        *llm.Client
}

func New(bot *internal.Bot, slackIntegration *slack_integration.Integration, llmClient *llm.Client) *reportWorker {
	return &reportWorker{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}
}

func (w *reportWorker) Work(ctx context.Context, job *river.Job[background.ReportWorkerArgs]) error {
	messages, err := schema.New(w.bot.DB).GetMessagesWithinTS(ctx, schema.GetMessagesWithinTSParams{
		ChannelID: job.Args.ChannelID,
		StartTs:   fmt.Sprintf("%d.000000", time.Now().AddDate(0, 0, -7).Unix()),
		EndTs:     fmt.Sprintf("%d.000000", time.Now().Unix()),
	})
	if err != nil {
		return fmt.Errorf("getting messages for channel: %w", err)
	}

	// TODO: Figure out how to handle bots and users in the same report
	userMsgCounts := make(map[string]int)
	botMsgCounts := make(map[string]int)
	incidentCounts := make(map[string]int)                // key: "service/alert"
	incidentDurations := make(map[string][]time.Duration) // key: "service/alert"

	for _, msg := range messages {
		if msg.Attrs.Message.BotID != "" {
			botMsgCounts[msg.Attrs.Message.BotUsername]++
		} else {
			userMsgCounts[msg.Attrs.Message.User]++
		}

		incidentKey := fmt.Sprintf("%s/%s", msg.Attrs.IncidentAction.Service, msg.Attrs.IncidentAction.Alert)

		switch msg.Attrs.IncidentAction.Action {
		case dto.ActionOpenIncident:
			incidentCounts[incidentKey]++
		case dto.ActionCloseIncident:
			incidentDurations[incidentKey] = append(incidentDurations[incidentKey], msg.Attrs.IncidentAction.Duration.Duration)
		}
	}

	textMessages := make([][]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Attrs.Message.BotID != "" {
			continue
		}

		threadMessages, err := schema.New(w.bot.DB).GetThreadMessages(ctx, schema.GetThreadMessagesParams{
			ChannelID: msg.ChannelID,
			ParentTs:  msg.Ts,
		})
		if err != nil {
			return fmt.Errorf("getting thread messages: %w", err)
		}
		fullThreadMessages := []string{msg.Attrs.Message.Text}
		for _, threadMessage := range threadMessages {
			fullThreadMessages = append(fullThreadMessages, threadMessage.Attrs.Message.Text)
		}

		textMessages = append(textMessages, fullThreadMessages)
	}

	suggestions, err := w.llmClient.GenerateChannelSuggestions(ctx, textMessages)
	if err != nil {
		return fmt.Errorf("generating suggestions: %w", err)
	}

	messageBlocks := w.formatSlackMessage(
		job.Args.ChannelID,
		userMsgCounts,
		botMsgCounts,
		incidentCounts,
		incidentDurations,
		suggestions,
	)
	return w.slackIntegration.PostMessage(ctx, job.Args.ChannelID, messageBlocks...)
}

func (w *reportWorker) formatSlackMessage(
	channelID string,
	userMsgCounts, botMsgCounts map[string]int,
	incidentCounts map[string]int,
	incidentDurations map[string][]time.Duration,
	suggestions string,
) []slack.Block {

	// Build report sections
	headerText := slack.NewTextBlockObject("mrkdwn",
		fmt.Sprintf("*Weekly Channel Report*\nChannel: <#%s>\nPeriod: %s - %s",
			channelID,
			time.Now().AddDate(0, 0, -7).Format("2006-01-02"),
			time.Now().Format("2006-01-02")),
		false, false)
	headerSection := slack.NewSectionBlock(headerText, nil, nil)

	// Users section
	var usersList strings.Builder
	for user, count := range sortMapByValue(userMsgCounts, 5) {
		usersList.WriteString(fmt.Sprintf("• <@%s>: %d messages\n", user, count))
	}
	usersText := slack.NewTextBlockObject("mrkdwn",
		fmt.Sprintf("*Top Active Users:*\n%s", usersList.String()),
		false, false)
	usersSection := slack.NewSectionBlock(usersText, nil, nil)

	// Bots section
	var botsList strings.Builder
	for bot, count := range sortMapByValue(botMsgCounts, 5) {
		botsList.WriteString(fmt.Sprintf("• <@%s>: %d messages\n", bot, count))
	}
	botsText := slack.NewTextBlockObject("mrkdwn",
		fmt.Sprintf("*Top Active Bots:*\n%s", botsList.String()),
		false, false)
	botsSection := slack.NewSectionBlock(botsText, nil, nil)

	// Alerts section
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

	// Sort alerts by service name first, then by alert name
	type alertEntry struct {
		service, alert string
		count          int
		duration       time.Duration
	}

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

	// Format suggestions
	var suggestionsSection *slack.SectionBlock
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

		suggestionsText := slack.NewTextBlockObject("mrkdwn",
			formattedSuggestions.String(),
			false, false)
		suggestionsSection = slack.NewSectionBlock(suggestionsText, nil, nil)
	}

	// Add signature
	signatureText := slack.NewTextBlockObject("mrkdwn",
		fmt.Sprintf("_Generated by Ratchet Bot at %s_",
			time.Now().Format("2006-01-02 15:04:05 MST")),
		false, false)
	signatureSection := slack.NewSectionBlock(signatureText, nil, nil)

	// Create divider
	divider := slack.NewDividerBlock()

	// Create all message components
	var messageBlocks []slack.Block

	// Add header and user/bot sections
	messageBlocks = append(messageBlocks,
		headerSection,
		divider,
		usersSection,
		divider,
		botsSection,
		divider,
	)

	// Add alerts section as a text block
	alertsHeaderText := slack.NewTextBlockObject("mrkdwn", "*Top Alerts:*", false, false)
	alertsHeaderSection := slack.NewSectionBlock(alertsHeaderText, nil, nil)
	alertsContentText := slack.NewTextBlockObject("mrkdwn", alertsTable.String(), false, false)
	alertsContentSection := slack.NewSectionBlock(alertsContentText, nil, nil)

	messageBlocks = append(messageBlocks,
		alertsHeaderSection,
		alertsContentSection,
	)

	// Add suggestions if present
	if suggestionsSection != nil {
		messageBlocks = append(messageBlocks,
			divider,
			suggestionsSection,
		)
	}

	// Add signature
	messageBlocks = append(messageBlocks,
		divider,
		signatureSection,
	)

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
