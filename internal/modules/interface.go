package modules

import (
	"context"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Handler interface {
	Name() string

	Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error
}
