// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0

package schema

import (
	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/pgvector/pgvector-go"
)

type ChannelsV2 struct {
	ID    string
	Attrs dto.ChannelAttrs
}

type IncidentRunbook struct {
	ID    int64
	Attrs dto.RunbookAttrs
}

type MessagesV2 struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
	Embedding *pgvector.Vector
}

type ThreadMessagesV2 struct {
	ChannelID string
	ParentTs  string
	Ts        string
	Attrs     dto.ThreadMessageAttrs
}
