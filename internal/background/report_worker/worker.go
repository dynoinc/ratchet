package report_worker

import (
	"context"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/olekukonko/tablewriter"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"
)

type reportWorker struct {
	river.WorkerDefaults[background.ReportWorkerArgs]

	db           *pgxpool.Pool
	slackClient  *slack.Client
	devChannelID string
}

func New(db *pgxpool.Pool, slackClient *slack.Client, devChannelID string) *reportWorker {
	return &reportWorker{
		db:           db,
		slackClient:  slackClient,
		devChannelID: devChannelID,
	}
}

func (w *reportWorker) Work(ctx context.Context, job *river.Job[background.ReportWorkerArgs]) error {
	messages, err := schema.New(w.db).GetMessagesWithinTS(ctx, schema.GetMessagesWithinTSParams{
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

	// Build report sections
	var report strings.Builder
	report.WriteString(fmt.Sprintf("*Weekly Channel Report (Channel: <#%s>, Period: %s-%s)*\n\n", job.Args.ChannelID, time.Now().AddDate(0, 0, -7).Format("2006-01-02"), time.Now().Format("2006-01-08")))

	// Top users section
	report.WriteString("*Top Active Users:*\n")
	for user, count := range sortMapByValue(userMsgCounts, 5) {
		report.WriteString(fmt.Sprintf("• <@%s>: %d messages\n", user, count))
	}
	report.WriteString("\n")

	// Top bots section
	report.WriteString("*Top Active Bots:*\n")
	for bot, count := range sortMapByValue(botMsgCounts, 5) {
		report.WriteString(fmt.Sprintf("• <@%s>: %d messages\n", bot, count))
	}
	report.WriteString("\n")

	// Top alerts section
	report.WriteString("*Top Alerts:*\n")
	report.WriteString("```\n")

	// Create table using go-pretty
	table := tablewriter.NewWriter(&report)
	table.SetHeader([]string{"Service", "Alert", "Occurrences", "Average Duration"})
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")

	for alert, count := range sortMapByValue(incidentCounts, 5) {
		service, alertName, _ := strings.Cut(alert, "/")
		avgDuration := calculateAverage(incidentDurations[alert])
		table.Append([]string{
			service,
			alertName,
			fmt.Sprintf("%d", count),
			avgDuration.Round(time.Second).String(),
		})
	}

	table.Render()
	report.WriteString("```\n")
	// Send report to Slack
	channelID := job.Args.ChannelID
	if w.devChannelID != "" {
		channelID = w.devChannelID
	}

	_, _, err = w.slackClient.PostMessage(channelID, slack.MsgOptionText(report.String(), false))
	if err != nil {
		return fmt.Errorf("posting report message: %w", err)
	}

	return nil
}

type kv struct {
	k string
	v int
}

// sortMapByValue sorts a map by value in descending order and returns the top i entries.
func sortMapByValue(userMsgCounts map[string]int, i int) iter.Seq2[string, int] {
	entries := make([]kv, 0, len(userMsgCounts))
	for k, v := range userMsgCounts {
		entries = append(entries, kv{k: k, v: v})
	}
	slices.SortFunc(entries, func(a, b kv) int {
		return b.v - a.v
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
