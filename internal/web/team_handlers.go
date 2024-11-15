package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type TeamPageData struct {
	ChannelID string
	Reports   []ReportListItem
}

type ReportListItem struct {
	ID          int64
	PeriodStart time.Time
	PeriodEnd   time.Time
	CreatedAt   time.Time
	WeekRange   string
}

type ReportDetailData struct {
	ChannelID      string
	WeekRange      string
	CreatedAt      time.Time
	ChannelName    string
	IncidentsTable template.HTML
	TopAlertsTable template.HTML
	MitigationTime string
}

func (h *httpHandlers) team(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("team")

	// Fetch only the necessary report metadata for listing
	reports, err := h.dbQueries.GetChannelReportsList(request.Context(), schema.GetChannelReportsListParams{
		ChannelID: channelID,
		Limit:     50,
	})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	var reportItems []ReportListItem
	for _, r := range reports {
		reportItems = append(reportItems, ReportListItem{
			ID:          int64(r.ID),
			PeriodStart: r.ReportPeriodStart.Time,
			PeriodEnd:   r.ReportPeriodEnd.Time,
			CreatedAt:   r.CreatedAt.Time,
			WeekRange:   formatDateRange(r.ReportPeriodStart.Time, r.ReportPeriodEnd.Time),
		})
	}

	data := TeamPageData{
		ChannelID: channelID,
		Reports:   reportItems,
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates.ExecuteTemplate(writer, "team.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) reportDetail(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("team")
	reportIDStr := request.PathValue("report")

	// Convert report ID from string to int32
	reportID64, err := strconv.ParseInt(reportIDStr, 10, 32)
	if err != nil {
		http.Error(writer, "Invalid report ID", http.StatusBadRequest)
		return
	}
	reportID := int32(reportID64)

	// Fetch the report
	r, err := h.dbQueries.GetReport(request.Context(), reportID)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse the report data
	var reportData report.ReportData
	if err := json.Unmarshal(r.ReportData, &reportData); err != nil {
		http.Error(writer, "Failed to parse report data", http.StatusInternalServerError)
		return
	}

	// Create a generator instance
	generator, err := report.NewGenerator()
	if err != nil {
		http.Error(writer, "Failed to create report generator", http.StatusInternalServerError)
		return
	}

	// Format the report for web display
	webReport := generator.FormatForWeb(&reportData)

	// Prepare the template data
	data := ReportDetailData{
		ChannelID:      channelID,
		WeekRange:      formatWeekRange(reportData.WeekRange),
		CreatedAt:      r.CreatedAt.Time,
		ChannelName:    webReport.ChannelName,
		IncidentsTable: template.HTML(formatIncidentsTable(webReport.Incidents)),
		TopAlertsTable: template.HTML(formatTopAlertsTable(webReport.TopAlerts)),
		MitigationTime: webReport.MitigationTime,
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates.ExecuteTemplate(writer, "report_detail.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func formatIncidentsTable(incidents []report.Incident) string {
	if len(incidents) == 0 {
		return "<p class='text-muted'>No incident data available</p>"
	}

	var html strings.Builder
	html.WriteString(`<table class="table table-bordered table-striped">
		<thead>
			<tr>
				<th>Severity</th>
				<th>Count</th>
				<th>Average Duration</th>
			</tr>
		</thead>
		<tbody>`)

	for _, incident := range incidents {
		html.WriteString(fmt.Sprintf(`
			<tr>
				<td>%s</td>
				<td>%d</td>
				<td>%s</td>
			</tr>`,
			incident.Severity,
			incident.Count,
			report.FormatDuration(incident.AverageTime),
		))
	}

	html.WriteString("</tbody></table>")
	return html.String()
}

func formatTopAlertsTable(alerts []report.Alert) string {
	if len(alerts) == 0 {
		return "<p class='text-muted'>No alerts data available</p>"
	}

	var html strings.Builder
	html.WriteString(`<table class="table table-bordered table-striped">
		<thead>
			<tr>
				<th>Alert</th>
				<th>Count</th>
				<th>Average Duration</th>
				<th>Last Seen</th>
			</tr>
		</thead>
		<tbody>`)

	for _, alert := range alerts {
		html.WriteString(fmt.Sprintf(`
			<tr>
				<td>%s</td>
				<td>%d</td>
				<td>%s</td>
				<td>%s</td>
			</tr>`,
			alert.Name,
			alert.Count,
			report.FormatDuration(alert.AverageTime),
			report.FormatTimeAgo(alert.LastSeen),
		))
	}

	html.WriteString("</tbody></table>")
	return html.String()
}

// Helper function to format week range
func formatWeekRange(dateRange report.DateRange) string {
	return dateRange.Start.Format("Jan 2") + " - " + dateRange.End.Format("Jan 2, 2006")
}

// Helper function to format date range without needing the full report data
func formatDateRange(start, end time.Time) string {
	return start.Format("Jan 2") + " - " + end.Format("Jan 2, 2006")
}
