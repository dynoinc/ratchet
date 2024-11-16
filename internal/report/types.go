package report

import "time"

// ReportData represents the core data structure of a report
type ReportData struct {
	ChannelName string     `json:"channel_name"`
	WeekRange   DateRange  `json:"week_range"`
	Incidents   []Incident `json:"incidents"`
	TopAlerts   []Alert    `json:"top_alerts"`
}

type DateRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type Incident struct {
	Severity    string        `json:"severity"`
	Count       int           `json:"count"`
	TotalTime   time.Duration `json:"total_time"`
	AverageTime time.Duration `json:"average_time"`
}

type Alert struct {
	Name        string        `json:"name"`
	Count       int           `json:"count"`
	LastSeen    time.Time     `json:"last_seen"`
	AverageTime time.Duration `json:"average_time"`
}

// Formatters for different output types
type SlackReport struct {
	ChannelName       string
	WeekRange         string
	IncidentsByType   string
	TopAlerts         string
	AvgMitigationTime string
}

type WebReport struct {
	ChannelName    string     `json:"channel_name"`
	PeriodStart    time.Time  `json:"period_start"`
	PeriodEnd      time.Time  `json:"period_end"`
	Incidents      []Incident `json:"incidents"`
	TopAlerts      []Alert    `json:"top_alerts"`
	MitigationTime string     `json:"mitigation_time"`
}
