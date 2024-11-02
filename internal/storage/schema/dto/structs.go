package dto

import "github.com/slack-go/slack/slackevents"

type ConversationAttrs struct {
}

type MessageAttrs struct {
	Upstream  slackevents.MessageEvent `json:"upstream"`
	Reactions map[string]int           `json:"reactions,omitempty"`
}
