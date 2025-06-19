package channel_report

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type alertEntry struct {
	Service  string        `json:"service"`
	Alert    string        `json:"alert"`
	Count    int           `json:"count"`
	Duration time.Duration `json:"duration"`
}

type ChannelReport struct {
	ChannelID         string                     `json:"channel_id"`
	StartDate         string                     `json:"start_date"`
	EndDate           string                     `json:"end_date"`
	Alerts            []alertEntry               `json:"alerts"`
	LongRunningAlerts []alertEntry               `json:"long_running_alerts"`
	UntriagedAlerts   []alertEntry               `json:"untriaged_alerts"`
	FrequentAlerts    []alertEntry               `json:"frequent_alerts"`
	RawMessages       [][]string                 `json:"raw_messages"`
	IncidentCounts    map[string]int             `json:"incident_counts"`
	IncidentDurations map[string][]time.Duration `json:"incident_durations"`
	TriageMsgCounts   map[string]int             `json:"triage_msg_counts"`
}

func Tool(db *schema.Queries) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.Tool{
		Name: "channel_report",
		Description: `Generate a comprehensive weekly channel report with incident analytics and raw message data.

FORMATTING INSTRUCTIONS:
When presenting this report, format it as a structured Slack message with the following sections:

1. **Header Section**
   - Title: "Weekly Channel Report"
   - Channel: <#CHANNEL_ID>
   - Period: YYYY-MM-DD to YYYY-MM-DD

2. **Top Alerts Section** (if alerts exist)
   - Section title: "Top Alerts:"
   - Format as a code block table with columns: SERVICE | ALERT | COUNT | AVG DURATION
   - Sort by count descending, then by service name, then by alert name
   - Take top 5 alerts
   - Truncate alert names longer than 30 characters with "..."

3. **Frequently Firing Alerts Section** (if alerts fired >5 times)
   - Section title: "Frequently Firing Alerts:"
   - Description: "The following alerts fired more than 5 times this week and may need attention:"
   - Format as a code block table with columns: SERVICE | ALERT | COUNT
   - Sort by count descending, take top 5
   - Add note: "Consider reviewing these alerts to reduce noise and improve signal."

4. **Long-Running Alerts Section** (if avg duration >72h)
   - Section title: "Alerts with Long Resolution Times:"
   - Description: "The following alerts consistently take more than 3 days to resolve and may need review:"
   - Format as a code block table with columns: SERVICE | ALERT | AVG DURATION
   - Sort by duration descending, take top 5
   - Add note: "Consider reviewing and potentially removing these alerts if they're not providing value."

5. **Untriaged Alerts Section** (if no triage activity)
   - Section title: "Alerts with No Triage Activity:"
   - Description: "The following alerts had no team interaction and may be unnecessary:"
   - Format as a code block table with columns: SERVICE | ALERT | COUNT
   - Sort by count descending, take top 5
   - Add note: "Consider removing these alerts as they don't seem to require team attention."

6. **Suggestions Section** (based on raw_messages analysis)
   - Section title: "Suggestions for Improvement:"
   - Analyze the raw_messages array to provide actionable insights
   - Format suggestions as bullet points with "• " prefix
   - Focus on patterns, communication improvements, and process optimizations`,
		InputSchema: mcp.ToolInputSchema{
			Type: "object",
			Properties: map[string]any{
				"channel_id": map[string]string{
					"type":        "string",
					"description": "The Slack channel ID to generate the report for",
				},
				"days": map[string]any{
					"type":        "integer",
					"description": "Number of days to look back for the report (default: 7)",
					"default":     7,
				},
			},
			Required: []string{"channel_id"},
		},
	}

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		channelID, err := request.RequireString("channel_id")
		if err != nil {
			return mcp.NewToolResultErrorf("channel_id parameter is required and must be a string: %v", err), nil
		}

		days := request.GetInt("days", 7)

		report, err := Generate(ctx, db, channelID, days)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to generate channel report", err), nil
		}

		jsonData, err := json.Marshal(report)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("failed to marshal report", err), nil
		}

		return mcp.NewToolResultText(string(jsonData)), nil
	}

	return tool, handler
}

func Generate(ctx context.Context, qtx *schema.Queries, channelID string, days int) (*ChannelReport, error) {
	startTime := time.Now().AddDate(0, 0, -days)
	endTime := time.Now()

	messages, err := qtx.GetMessagesWithinTS(ctx, schema.GetMessagesWithinTSParams{
		ChannelID: channelID,
		StartTs:   fmt.Sprintf("%d.000000", startTime.Unix()),
		EndTs:     fmt.Sprintf("%d.000000", endTime.Unix()),
	})
	if err != nil {
		return nil, fmt.Errorf("getting messages for channel: %w", err)
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
				return nil, fmt.Errorf("getting thread messages: %w", err)
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
			LimitVal:  10,
		})
		if err != nil {
			return nil, fmt.Errorf("getting thread messages: %w", err)
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

	// Compute long-running alerts
	var longRunningAlerts []alertEntry
	for alert, durations := range incidentDurations {
		avgDuration := calculateAverage(durations)
		if avgDuration > 72*time.Hour { // 3 days
			service, alertName, _ := strings.Cut(alert, "/")
			longRunningAlerts = append(longRunningAlerts, alertEntry{
				Service:  service,
				Alert:    alertName,
				Count:    incidentCounts[alert],
				Duration: avgDuration,
			})
		}
	}
	// Sort by duration descending and take top 5
	slices.SortFunc(longRunningAlerts, func(a, b alertEntry) int {
		return int(b.Duration.Seconds()) - int(a.Duration.Seconds())
	})
	if len(longRunningAlerts) > 5 {
		longRunningAlerts = longRunningAlerts[:5]
	}

	// Compute untriaged alerts
	var untriagedAlerts []alertEntry
	for alert, count := range incidentCounts {
		if triageMsgCounts[alert] == 0 {
			service, alertName, _ := strings.Cut(alert, "/")
			untriagedAlerts = append(untriagedAlerts, alertEntry{
				Service: service,
				Alert:   alertName,
				Count:   count,
			})
		}
	}
	// Sort by count descending and take top 5
	slices.SortFunc(untriagedAlerts, func(a, b alertEntry) int {
		return b.Count - a.Count
	})
	if len(untriagedAlerts) > 5 {
		untriagedAlerts = untriagedAlerts[:5]
	}

	// Compute frequently firing alerts
	var frequentAlerts []alertEntry
	for alert, count := range incidentCounts {
		if count > 5 {
			service, alertName, _ := strings.Cut(alert, "/")
			frequentAlerts = append(frequentAlerts, alertEntry{
				Service: service,
				Alert:   alertName,
				Count:   count,
			})
		}
	}
	// Sort by count descending and take top 5
	slices.SortFunc(frequentAlerts, func(a, b alertEntry) int {
		return b.Count - a.Count
	})
	if len(frequentAlerts) > 5 {
		frequentAlerts = frequentAlerts[:5]
	}

	// Build alerts for the main alerts section
	var alerts []alertEntry
	for alert, count := range incidentCounts {
		service, alertName, _ := strings.Cut(alert, "/")
		avgDuration := calculateAverage(incidentDurations[alert])
		alerts = append(alerts, alertEntry{
			Service:  service,
			Alert:    alertName,
			Count:    count,
			Duration: avgDuration,
		})
	}

	// Sort alerts by count (descending), then by service name, then by alert name
	slices.SortFunc(alerts, func(a, b alertEntry) int {
		if a.Count != b.Count {
			return b.Count - a.Count // Descending order
		}
		if a.Service != b.Service {
			return strings.Compare(a.Service, b.Service)
		}
		return strings.Compare(a.Alert, b.Alert)
	})

	// Take top 5
	if len(alerts) > 5 {
		alerts = alerts[:5]
	}

	return &ChannelReport{
		ChannelID:         channelID,
		StartDate:         startTime.Format("2006-01-02"),
		EndDate:           endTime.Format("2006-01-02"),
		Alerts:            alerts,
		LongRunningAlerts: longRunningAlerts,
		UntriagedAlerts:   untriagedAlerts,
		FrequentAlerts:    frequentAlerts,
		RawMessages:       textMessages,
		IncidentCounts:    incidentCounts,
		IncidentDurations: incidentDurations,
		TriageMsgCounts:   triageMsgCounts,
	}, nil
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
