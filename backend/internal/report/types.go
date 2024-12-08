package report

import (
	"time"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

// ReportData represents the core data structure of a report

// Formatters for different output types
type SlackReport struct {
	ChannelName       string
	WeekRange         string
	IncidentsByType   string
	TopAlerts         string
	AvgMitigationTime string
}

type WebReport struct {
	ChannelName    string         `json:"channel_name"`
	PeriodStart    time.Time      `json:"period_start"`
	PeriodEnd      time.Time      `json:"period_end"`
	Incidents      []dto.Incident `json:"incidents"`
	TopAlerts      []dto.Alert    `json:"top_alerts"`
	MitigationTime string         `json:"mitigation_time"`
}
