package dto

import (
	"time"

	"github.com/slack-go/slack"
)

type MessageAttrs struct {
	Message *slack.Message `json:"message,omitempty"`

	// Set in case this is an incident open/close message
	IncidentID int32  `json:"incident_id,omitempty"`
	Action     string `json:"action,omitempty"`

	// Set in case this is a bot message
	BotName string `json:"bot_name,omitempty"`

	// Set in case this is a message from a user
	UserID string `json:"user_id,omitempty"`
}

type IncidentAttrs struct {
}

type ChannelAttrs struct {
	Name string `json:"name,omitempty"`
}

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
