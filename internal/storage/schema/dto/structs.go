package dto

import (
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

type MessageAttrs struct {
	Upstream slackevents.MessageEvent `json:"upstream"`
	Message  slack.Message            `json:"message"`
}

type IncidentAttrs struct {
}
