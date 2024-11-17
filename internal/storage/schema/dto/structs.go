package dto

import (
	"encoding/json"
	"time"

	"github.com/slack-go/slack"
)

type MessageAttrs struct {
	Message *slack.Message `json:"message,omitempty"`

	// Set in case this is a incident open/close message
	IncidentID int32  `json:"incident_id,omitempty"`
	Action     string `json:"action,omitempty"`

	// Set in case this is a bot message
	BotName string `json:"bot_name,omitempty"`

	// Set in case this is a message from a user
	UserID string `json:"user_id,omitempty"`
}

type IncidentAttrs struct {
}

type ReportView struct {
	ID                int32           `json:"id"`
	ChannelID         string          `json:"channel_id"`
	ChannelName       string          `json:"channel_name"`
	ReportPeriodStart time.Time       `json:"report_period_start"`
	ReportPeriodEnd   time.Time       `json:"report_period_end"`
	ReportData        json.RawMessage `json:"report_data"`
	CreatedAt         time.Time       `json:"created_at"`
}

type ChannelAttrs struct {
	Name string `json:"name,omitempty"`
}
