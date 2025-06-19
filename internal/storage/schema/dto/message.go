package dto

type SlackMessage struct {
	SubType     string `json:"subtype,omitzero"`
	Text        string `json:"text,omitzero"`
	User        string `json:"user,omitzero"`
	BotID       string `json:"bot_id,omitzero"`
	BotUsername string `json:"bot_usernames,omitzero"`
}

type MessageAttrs struct {
	Message   SlackMessage   `json:"message,omitzero"`
	Reactions map[string]int `json:"reactions,omitzero"`

	// Other information stored by modules
	IncidentAction IncidentAction `json:"incident_action,omitzero"` // from classifier binary
}
