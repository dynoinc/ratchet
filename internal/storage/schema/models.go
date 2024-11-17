// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0

package schema

import (
	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
)

type Channel struct {
	ChannelID        string
	ChannelName      pgtype.Text
	CreatedAt        pgtype.Timestamptz
	SlackTsWatermark string
}

type Incident struct {
	IncidentID     int32
	ChannelID      string
	SlackTs        string
	Alert          string
	Service        string
	Priority       string
	Attrs          dto.IncidentAttrs
	StartTimestamp pgtype.Timestamptz
	EndTimestamp   pgtype.Timestamptz
}

type Message struct {
	ChannelID string
	SlackTs   string
	Attrs     dto.MessageAttrs
}

type Report struct {
	ID                int32
	ChannelID         string
	ReportPeriodStart pgtype.Timestamptz
	ReportPeriodEnd   pgtype.Timestamptz
	ReportData        []byte
	CreatedAt         pgtype.Timestamptz
}
