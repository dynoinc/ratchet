package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/jackc/pgx/v5/pgtype"
)

// Template functions map
var templateFuncs = template.FuncMap{
	"FormatDuration":  report.FormatDuration,
	"FormatTimeAgo":   report.FormatTimeAgo,
	"formatDateRange": formatDateRange,
}

type TeamPageData struct {
	ChannelID   string
	ChannelName string
	Reports     []ReportListItem
}

type ReportListItem struct {
	ID          int64
	ChannelName string
	PeriodStart time.Time
	PeriodEnd   time.Time
	CreatedAt   time.Time
	WeekRange   string
}

type ReportDetailData struct {
	ChannelID      string
	ChannelName    string
	WeekRange      string
	CreatedAt      time.Time
	Incidents      []report.Incident
	TopAlerts      []report.Alert
	MitigationTime string
}

func (h *httpHandlers) team(writer http.ResponseWriter, request *http.Request) {
	channelName := request.PathValue("team")

	channel, err := h.dbQueries.GetChannelByName(request.Context(), pgtype.Text{
		String: channelName,
		Valid:  true,
	})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	reports, err := h.dbQueries.GetChannelReports(request.Context(), schema.GetChannelReportsParams{
		ChannelID: channel.ChannelID,
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
			ChannelName: r.ChannelName.String,
			PeriodStart: r.ReportPeriodStart.Time,
			PeriodEnd:   r.ReportPeriodEnd.Time,
			CreatedAt:   r.CreatedAt.Time,
			WeekRange:   formatDateRange(r.ReportPeriodStart.Time, r.ReportPeriodEnd.Time),
		})
	}

	data := TeamPageData{
		ChannelID:   channel.ChannelID,
		ChannelName: channel.ChannelName.String,
		Reports:     reportItems,
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates.ExecuteTemplate(writer, "team.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (h *httpHandlers) reportDetail(writer http.ResponseWriter, request *http.Request) {
	channelName := request.PathValue("team")
	reportIDStr := request.PathValue("report")

	channel, err := h.dbQueries.GetChannelByName(request.Context(), pgtype.Text{
		String: channelName,
		Valid:  true,
	})
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

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

	if r.ChannelID != channel.ChannelID {
		http.Error(writer, "Not found", http.StatusNotFound)
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
		ChannelID:      channel.ChannelID,
		ChannelName:    r.ChannelName.String,
		WeekRange:      formatWeekRange(reportData.WeekRange),
		CreatedAt:      r.CreatedAt.Time,
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
