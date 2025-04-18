package modules

import (
	"context"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Handler interface {
	Name() string

	OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error
}

type ThreadHandler interface {
	OnThreadMessage(ctx context.Context, channelID string, slackTS string, threadTS string, msg dto.MessageAttrs) error
}
