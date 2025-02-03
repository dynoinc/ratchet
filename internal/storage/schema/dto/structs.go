package dto

type SlackMessage struct {
	SubType     string `json:"subtype,omitzero"`
	Text        string `json:"text,omitzero"`
	User        string `json:"user,omitzero"`
	BotID       string `json:"bot_id,omitzero"`
	BotUsername string `json:"bot_usernames,omitzero"`
}

type ChannelAttrs struct {
	Name string `json:"name,omitzero"`
}

type MessageAttrs struct {
	Message        SlackMessage   `json:"message,omitzero"`
	IncidentAction IncidentAction `json:"incident_action,omitzero"`
}

type ThreadMessageAttrs struct {
	Message SlackMessage `json:"message,omitzero"`
}
