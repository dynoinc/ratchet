package dto

import (
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

type MessageAttrs struct {
	Upstream *slackevents.MessageEvent `json:"upstream,omitempty"`
	Message  *slack.Message            `json:"message,omitempty"`
}

type IncidentAttrs struct {
}
