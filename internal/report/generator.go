package report

import (
	"bytes"
	"fmt"
	"time"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/olekukonko/tablewriter"
)

type Generator struct{}

func NewGenerator() (*Generator, error) {
	return &Generator{}, nil
}

// GenerateReportData creates the core report data structure
func (g *Generator) GenerateReportData(
	channelName string,
	startDate time.Time,
	dbIncidentStats []schema.GetIncidentStatsByPeriodRow,
	dbTopAlerts []schema.GetTopAlertsRow) (*ReportData, error) {

	incidents := make([]Incident, len(dbIncidentStats))
	for i, stat := range dbIncidentStats {
		avgDuration := time.Duration(stat.AvgDurationSeconds) * time.Second
		totalDuration := time.Duration(stat.TotalDurationSeconds) * time.Second

		incidents[i] = Incident{
			Severity:    stat.Severity,
			Count:       int(stat.Count),
			TotalTime:   totalDuration,
			AverageTime: avgDuration,
		}
	}

	alerts := make([]Alert, len(dbTopAlerts))
	for i, alert := range dbTopAlerts {
		avgDuration := time.Duration(alert.AvgDurationSeconds) * time.Second

		alerts[i] = Alert{
			Name:        alert.Alert,
			Count:       int(alert.Count),
			LastSeen:    alert.LastSeen.(time.Time),
			AverageTime: avgDuration,
		}
	}

	return &ReportData{
		ChannelName: channelName,
		WeekRange: DateRange{
			Start: startDate,
			End:   startDate.AddDate(0, 0, 6),
		},
		Incidents: incidents,
		TopAlerts: alerts,
	}, nil
}

// FormatForSlack formats the report data for Slack display
func (g *Generator) FormatForSlack(data *ReportData) *SlackReport {
	return &SlackReport{
		ChannelName:       data.ChannelName,
		WeekRange:         formatWeekRange(data.WeekRange),
		IncidentsByType:   g.generateIncidentsTable(data.Incidents),
		TopAlerts:         g.generateTopAlertsTable(data.TopAlerts),
		AvgMitigationTime: calculateAvgMitigationTime(data.Incidents),
	}
}

// FormatForWeb formats the report data for web display
func (g *Generator) FormatForWeb(data *ReportData) *WebReport {
	return &WebReport{
		ChannelName:    data.ChannelName,
		PeriodStart:    data.WeekRange.Start,
		PeriodEnd:      data.WeekRange.End,
		Incidents:      data.Incidents,
		TopAlerts:      data.TopAlerts,
		MitigationTime: calculateAvgMitigationTime(data.Incidents),
	}
}

// Helper functions for Slack formatting
func (g *Generator) generateIncidentsTable(incidents []Incident) string {
	var buf bytes.Buffer
	table := tablewriter.NewWriter(&buf)

	table.SetHeader([]string{"SEVERITY", "COUNT", "AVG TIME"})
	table.SetBorders(tablewriter.Border{Left: true, Top: true, Right: true, Bottom: true})
	table.SetCenterSeparator("|")
	table.SetColumnSeparator("|")
	table.SetRowSeparator("-")
	table.SetAlignment(tablewriter.ALIGN_CENTER)
	table.SetHeaderAlignment(tablewriter.ALIGN_CENTER)

	for _, incident := range incidents {
		table.Append([]string{
			incident.Severity,
			fmt.Sprintf("%d", incident.Count),
			FormatDuration(incident.AverageTime),
		})
	}

	table.Render()
	return buf.String()
}

func (g *Generator) generateTopAlertsTable(alerts []Alert) string {
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
			alert.Name,
			fmt.Sprintf("%d", alert.Count),
			FormatDuration(alert.AverageTime),
			FormatTimeAgo(alert.LastSeen),
		})
	}

	table.Render()
	return buf.String()
}

// Helper functions
func formatWeekRange(dateRange DateRange) string {
	return dateRange.Start.Format("Jan 2") + " - " + dateRange.End.Format("Jan 2, 2006")
}

func calculateAvgMitigationTime(incidents []Incident) string {
	var totalTime time.Duration
	var totalCount int
	for _, incident := range incidents {
		totalTime += incident.TotalTime
		totalCount += incident.Count
	}
	if totalCount == 0 {
		return "0m"
	}
	avgTime := totalTime / time.Duration(totalCount)
	return FormatDuration(avgTime)
}

func FormatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func FormatTimeAgo(t time.Time) string {
	duration := time.Since(t)
	hours := int(duration.Hours())

	if hours < 24 {
		return fmt.Sprintf("%dh ago", hours)
	}
	days := hours / 24
	return fmt.Sprintf("%dd ago", days)
}
