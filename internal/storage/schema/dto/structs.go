package dto

type SlackMessage struct {
	SubType     string `json:"subtype,omitzero"`
	Text        string `json:"text,omitzero"`
	User        string `json:"user,omitzero"`
	BotID       string `json:"bot_id,omitzero"`
	BotUsername string `json:"bot_usernames,omitzero"`
}

type MessageAttrs struct {
	Message        SlackMessage   `json:"message,omitzero"`
	IncidentAction IncidentAction `json:"incident_action,omitzero"`
}

type ThreadMessageAttrs struct {
	Message SlackMessage `json:"message,omitzero"`
}

type RunbookAttrs struct {
	ServiceName string `json:"service_name"`
	AlertName   string `json:"alert_name"`
	Runbook     string `json:"runbook"`
}
