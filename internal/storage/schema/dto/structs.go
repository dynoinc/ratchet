package dto

import "github.com/slack-go/slack/slackevents"

type ConversationAttrs struct {
}

type MessageAttrs struct {
	Upstream slackevents.MessageEvent `json:"upstream"`
}
