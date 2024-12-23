package report

import (
	"time"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

// GenerateReportData creates the core report data structure
func GenerateReportData(
	channelName string,
	startDate time.Time,
	endDate time.Time,
	dbIncidentStats []schema.GetIncidentStatsByPeriodRow,
	dbTopAlerts []schema.GetTopAlertsRow) (*dto.ReportData, error) {

	incidents := make([]dto.Incident, len(dbIncidentStats))
	for i, stat := range dbIncidentStats {
		avgDuration := time.Duration(stat.AvgDurationSeconds) * time.Second
		totalDuration := time.Duration(stat.TotalDurationSeconds) * time.Second

		incidents[i] = dto.Incident{
			Severity:    stat.Severity,
			Count:       int(stat.Count),
			TotalTime:   totalDuration,
			AverageTime: avgDuration,
		}
	}

	alerts := make([]dto.Alert, len(dbTopAlerts))
	for i, alert := range dbTopAlerts {
		avgDuration := time.Duration(alert.AvgDurationSeconds) * time.Second

		alerts[i] = dto.Alert{
			Name:        alert.Alert,
			Count:       int(alert.Count),
			LastSeen:    alert.LastSeen.(time.Time),
			AverageTime: avgDuration,
		}
	}

	return &dto.ReportData{
		ChannelName: channelName,
		WeekRange: dto.DateRange{
			Start: startDate,
			End:   endDate,
		},
		Incidents: incidents,
		TopAlerts: alerts,
	}, nil
}
