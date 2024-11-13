package report

import (
	"bytes"
	"fmt"
	"time"

	"github.com/olekukonko/tablewriter"

	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Report struct {
	ChannelName       string
	WeekRange         string
	IncidentsByType   string
	TopAlerts         string
	AvgMitigationTime string
}

type IncidentStats struct {
	Severity    string
	Count       int
	TotalTime   time.Duration
	AverageTime time.Duration
}

type AlertStats struct {
	AlertName   string
	Count       int
	LastSeen    time.Time
	AverageTime time.Duration
}

type Generator struct{}

func NewGenerator() (*Generator, error) {
	return &Generator{}, nil
}

// Convert database rows to our report structures
func convertIncidentStats(dbStats []schema.GetIncidentStatsByPeriodRow) []IncidentStats {
	stats := make([]IncidentStats, len(dbStats))
	for i, stat := range dbStats {
		avgDuration := time.Duration(stat.AvgDurationSeconds) * time.Second
		totalDuration := time.Duration(stat.TotalDurationSeconds) * time.Second

		stats[i] = IncidentStats{
			Severity:    stat.Severity,
			Count:       int(stat.Count),
			TotalTime:   totalDuration,
			AverageTime: avgDuration,
		}
	}
	return stats
}

func convertTopAlerts(dbAlerts []schema.GetTopAlertsRow) []AlertStats {
	alerts := make([]AlertStats, len(dbAlerts))
	for i, alert := range dbAlerts {
		avgDuration := time.Duration(alert.AvgDurationSeconds) * time.Second

		alerts[i] = AlertStats{
			AlertName:   alert.Alert,
			Count:       int(alert.Count),
			LastSeen:    alert.LastSeen.(time.Time),
			AverageTime: avgDuration,
		}
	}
	return alerts
}

func (g *Generator) GenerateReport(
	channelName string,
	startDate time.Time,
	dbIncidentStats []schema.GetIncidentStatsByPeriodRow,
	dbTopAlerts []schema.GetTopAlertsRow) (Report, error) {
	// Convert database rows to our structures
	incidentStats := convertIncidentStats(dbIncidentStats)
	topAlerts := convertTopAlerts(dbTopAlerts)

	return Report{
		ChannelName:       channelName,
		WeekRange:         formatWeekRange(startDate),
		IncidentsByType:   g.generateIncidentsTable(incidentStats),
		TopAlerts:         g.generateTopAlertsTable(topAlerts),
		AvgMitigationTime: formatDuration(calculateAverageMitigationTime(incidentStats)),
	}, nil
}

func (g *Generator) generateIncidentsTable(stats []IncidentStats) string {
	var buf bytes.Buffer
	table := tablewriter.NewWriter(&buf)

	table.SetHeader([]string{"SEVERITY", "COUNT", "AVG TIME"})
	table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
	table.SetCenterSeparator("|")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")
	table.SetAlignment(tablewriter.ALIGN_CENTER)
	table.SetHeaderAlignment(tablewriter.ALIGN_CENTER)

	for _, stat := range stats {
		table.Append([]string{
			stat.Severity,
			fmt.Sprintf("%d", stat.Count),
			formatDuration(stat.AverageTime),
		})
	}

	table.Render()
	return buf.String()
}

func (g *Generator) generateTopAlertsTable(alerts []AlertStats) string {
	var buf bytes.Buffer
	table := tablewriter.NewWriter(&buf)

	table.SetHeader([]string{"ALERT", "COUNT", "AVG TIME", "LAST SEEN"})
	table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
	table.SetCenterSeparator("|")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderAlignment(tablewriter.ALIGN_CENTER)
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_LEFT,
		tablewriter.ALIGN_CENTER,
		tablewriter.ALIGN_CENTER,
		tablewriter.ALIGN_CENTER,
	})

	for _, alert := range alerts {
		table.Append([]string{
			alert.AlertName,
			fmt.Sprintf("%d", alert.Count),
			formatDuration(alert.AverageTime),
			formatTimeAgo(alert.LastSeen),
		})
	}

	table.Render()
	return buf.String()
}

func calculateAverageMitigationTime(stats []IncidentStats) time.Duration {
	var totalTime time.Duration
	var totalCount int
	for _, stat := range stats {
		totalTime += stat.TotalTime
		totalCount += stat.Count
	}
	if totalCount == 0 {
		return 0
	}
	return totalTime / time.Duration(totalCount)
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func formatTimeAgo(t time.Time) string {
	duration := time.Since(t)
	hours := int(duration.Hours())

	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("%dd ago", days)
}

func formatWeekRange(startDate time.Time) string {
	endDate := startDate.AddDate(0, 0, 6)
	return startDate.Format("Jan 2") + " - " + endDate.Format("Jan 2, 2006")
}
