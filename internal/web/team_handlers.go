package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

// Template functions map
var templateFuncs = template.FuncMap{
	"FormatDuration":  report.FormatDuration,
	"FormatTimeAgo":   report.FormatTimeAgo,
	"formatDateRange": formatDateRange,
}

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
	Incidents      []report.Incident
	TopAlerts      []report.Alert
	MitigationTime string
}

func (h *httpHandlers) team(writer http.ResponseWriter, request *http.Request) {
	channelID := request.PathValue("team")

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

	reportID64, err := strconv.ParseInt(reportIDStr, 10, 32)
	if err != nil {
		http.Error(writer, "Invalid report ID", http.StatusBadRequest)
		return
	}
	reportID := int32(reportID64)

	r, err := h.dbQueries.GetReport(request.Context(), reportID)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	var reportData report.ReportData
	if err := json.Unmarshal(r.ReportData, &reportData); err != nil {
		http.Error(writer, "Failed to parse report data", http.StatusInternalServerError)
		return
	}

	generator, err := report.NewGenerator()
	if err != nil {
		http.Error(writer, "Failed to create report generator", http.StatusInternalServerError)
		return
	}

	webReport := generator.FormatForWeb(&reportData)

	data := ReportDetailData{
		ChannelID:      channelID,
		WeekRange:      formatWeekRange(reportData.WeekRange),
		CreatedAt:      r.CreatedAt.Time,
		ChannelName:    webReport.ChannelName,
		Incidents:      webReport.Incidents,
		TopAlerts:      webReport.TopAlerts,
		MitigationTime: webReport.MitigationTime,
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates.ExecuteTemplate(writer, "report_detail.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Helper function to format date range
func formatDateRange(start, end time.Time) string {
	return start.Format("Jan 2") + " - " + end.Format("Jan 2, 2006")
}

// Helper function to format week range from DateRange
func formatWeekRange(dateRange report.DateRange) string {
	return formatDateRange(dateRange.Start, dateRange.End)
}
