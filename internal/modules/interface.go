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
	OnThreadMessage(ctx context.Context, channelID string, slackTS string, parentTS string, msg dto.MessageAttrs) error
}

type OnBackfillMessage interface {
	EnabledForBackfill() bool
}
