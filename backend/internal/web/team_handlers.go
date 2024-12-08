package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dynoinc/ratchet/internal/report"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
)

// Template functions map
var templateFuncs = template.FuncMap{
	"FormatDuration":  report.FormatDuration,
	"FormatTimeAgo":   report.FormatTimeAgo,
	"formatDateRange": formatDateRange,
	"unmarshalAttrs": func(attrsJSON []byte) *dto.ChannelAttrs {
		var attrs dto.ChannelAttrs
		if err := json.Unmarshal(attrsJSON, &attrs); err != nil {
			return nil
		}
		return &attrs
	},
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
	Incidents      []dto.Incident
	TopAlerts      []dto.Alert
	MitigationTime string
}

func (h *httpHandlers) team(writer http.ResponseWriter, request *http.Request) {
	channelName := request.PathValue("team")

	// Get channel by name using the JSONB attrs
	channel, err := h.dbQueries.GetChannelByName(request.Context(), channelName)
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
			ChannelName: channel.Attrs.Name,
			PeriodStart: r.ReportPeriodStart.Time,
			PeriodEnd:   r.ReportPeriodEnd.Time,
			CreatedAt:   r.CreatedAt.Time,
			WeekRange:   formatDateRange(r.ReportPeriodStart.Time, r.ReportPeriodEnd.Time),
		})
	}

	data := TeamPageData{
		ChannelID:   channel.ChannelID,
		ChannelName: channel.Attrs.Name,
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

	channel, err := h.dbQueries.GetChannelByName(request.Context(), channelName)
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

	generator, err := report.NewGenerator()
	if err != nil {
		http.Error(writer, "Failed to create report generator", http.StatusInternalServerError)
		return
	}

	webReport := generator.FormatForWeb(&r.ReportData)

	data := ReportDetailData{
		ChannelID:      channel.ChannelID,
		ChannelName:    channel.Attrs.Name,
		WeekRange:      formatWeekRange(r.ReportData.WeekRange),
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
func formatWeekRange(dateRange dto.DateRange) string {
	return formatDateRange(dateRange.Start, dateRange.End)
}

func (h *httpHandlers) instantReport(writer http.ResponseWriter, request *http.Request) {
	channelName := request.PathValue("team")

	// Get channel by name
	channel, err := h.dbQueries.GetChannelByName(request.Context(), channelName)
	if err != nil {
		http.Error(writer, "Channel not found", http.StatusNotFound)
		return
	}

	// Calculate the time range for the report
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -7) // Last 7 days

	// Get incident statistics from database
	incidentStats, err := h.dbQueries.GetIncidentStatsByPeriod(request.Context(), schema.GetIncidentStatsByPeriodParams{
		ChannelID: channel.ChannelID,
		StartTimestamp: pgtype.Timestamptz{
			Time:  startDate,
			Valid: true,
		},
		StartTimestamp_2: pgtype.Timestamptz{
			Time:  endDate,
			Valid: true,
		},
	})
	if err != nil {
		http.Error(writer, "Failed to get incident stats", http.StatusInternalServerError)
		return
	}

	// Get top alerts from database
	topAlerts, err := h.dbQueries.GetTopAlerts(request.Context(), schema.GetTopAlertsParams{
		ChannelID: channel.ChannelID,
		StartTimestamp: pgtype.Timestamptz{
			Time:  startDate,
			Valid: true,
		},
		StartTimestamp_2: pgtype.Timestamptz{
			Time:  endDate,
			Valid: true,
		},
	})
	if err != nil {
		http.Error(writer, "Failed to get top alerts", http.StatusInternalServerError)
		return
	}

	// Create report generator
	generator, err := report.NewGenerator()
	if err != nil {
		http.Error(writer, "Failed to create report generator", http.StatusInternalServerError)
		return
	}

	// Generate the report using the database data
	reportData, err := generator.GenerateReportData(channelName, startDate, incidentStats, topAlerts)
	if err != nil {
		http.Error(writer, "Failed to generate report", http.StatusInternalServerError)
		return
	}

	// If it's an API request (from frontend), return JSON
	if strings.HasPrefix(request.URL.Path, "/api/") {
		webReport := generator.FormatForWeb(reportData)
		response := struct {
			ChannelID      string         `json:"channelId"`
			ChannelName    string         `json:"channelName"`
			WeekRange      string         `json:"weekRange"`
			CreatedAt      time.Time      `json:"createdAt"`
			Incidents      []dto.Incident `json:"incidents"`
			TopAlerts      []dto.Alert    `json:"topAlerts"`
			MitigationTime string         `json:"mitigationTime"`
		}{
			ChannelID:      channel.ChannelID,
			ChannelName:    channelName,
			WeekRange:      formatDateRange(startDate, endDate),
			CreatedAt:      time.Now(),
			Incidents:      webReport.Incidents,
			TopAlerts:      webReport.TopAlerts,
			MitigationTime: webReport.MitigationTime,
		}

		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			http.Error(writer, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	// For web requests, render the HTML template
	data := ReportDetailData{
		ChannelID:      channel.ChannelID,
		ChannelName:    channelName,
		WeekRange:      formatDateRange(startDate, endDate),
		CreatedAt:      time.Now(),
		Incidents:      generator.FormatForWeb(reportData).Incidents,
		TopAlerts:      generator.FormatForWeb(reportData).TopAlerts,
		MitigationTime: generator.FormatForWeb(reportData).MitigationTime,
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = h.templates.ExecuteTemplate(writer, "report_detail.html", data)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
}
