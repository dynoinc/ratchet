package dto

import "github.com/slack-go/slack/slackevents"

type MessageAttrs struct {
	Upstream slackevents.MessageEvent `json:"upstream"`
}

type IncidentAttrs struct {
}
