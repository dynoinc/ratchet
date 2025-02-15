package modules

import (
	"context"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Handler interface {
	Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error
}

type HandlerFunc func(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error

func (f HandlerFunc) Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	return f(ctx, channelID, slackTS, msg)
}
