// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: messages.sql

package schema

import (
	"context"

	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

const addMessage = `-- name: AddMessage :exec
INSERT INTO messages (channel_id, slack_ts, attrs)
VALUES ($1, $2, $3)
ON CONFLICT (channel_id, slack_ts) DO NOTHING
`

type AddMessageParams struct {
	ChannelID string
	SlackTs   string
	Attrs     dto.MessageAttrs
}

func (q *Queries) AddMessage(ctx context.Context, arg AddMessageParams) error {
	_, err := q.db.Exec(ctx, addMessage, arg.ChannelID, arg.SlackTs, arg.Attrs)
	return err
}

const deleteOldMessages = `-- name: DeleteOldMessages :exec
DELETE FROM messages
WHERE to_timestamp(slack_ts::double precision) < (now() - interval '3 months')
`

func (q *Queries) DeleteOldMessages(ctx context.Context) error {
	_, err := q.db.Exec(ctx, deleteOldMessages)
	return err
}

const getAllMessages = `-- name: GetAllMessages :many
SELECT channel_id, slack_ts, attrs
FROM messages
WHERE channel_id = $1
`

func (q *Queries) GetAllMessages(ctx context.Context, channelID string) ([]Message, error) {
	rows, err := q.db.Query(ctx, getAllMessages, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Message
	for rows.Next() {
		var i Message
		if err := rows.Scan(&i.ChannelID, &i.SlackTs, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMessage = `-- name: GetMessage :one
SELECT channel_id, slack_ts, attrs
FROM messages
WHERE channel_id = $1
  AND slack_ts = $2
`

type GetMessageParams struct {
	ChannelID string
	SlackTs   string
}

func (q *Queries) GetMessage(ctx context.Context, arg GetMessageParams) (Message, error) {
	row := q.db.QueryRow(ctx, getMessage, arg.ChannelID, arg.SlackTs)
	var i Message
	err := row.Scan(&i.ChannelID, &i.SlackTs, &i.Attrs)
	return i, err
}

const setIncidentID = `-- name: SetIncidentID :exec
UPDATE messages
SET attrs = attrs || jsonb_build_object('incident_id', $1::integer, 'action', $2::text)
WHERE channel_id = $3
  AND slack_ts = $4
`

type SetIncidentIDParams struct {
	IncidentID int32
	Action     string
	ChannelID  string
	SlackTs    string
}

func (q *Queries) SetIncidentID(ctx context.Context, arg SetIncidentIDParams) error {
	_, err := q.db.Exec(ctx, setIncidentID,
		arg.IncidentID,
		arg.Action,
		arg.ChannelID,
		arg.SlackTs,
	)
	return err
}

const tagAsBotNotification = `-- name: TagAsBotNotification :exec
UPDATE messages
SET attrs = attrs || jsonb_build_object('bot_name', $1::text)
WHERE channel_id = $2
  AND slack_ts = $3
`

type TagAsBotNotificationParams struct {
	BotName   string
	ChannelID string
	SlackTs   string
}

func (q *Queries) TagAsBotNotification(ctx context.Context, arg TagAsBotNotificationParams) error {
	_, err := q.db.Exec(ctx, tagAsBotNotification, arg.BotName, arg.ChannelID, arg.SlackTs)
	return err
}

const tagAsUserMessage = `-- name: TagAsUserMessage :exec
UPDATE messages
SET attrs = attrs || jsonb_build_object('user_id', $1::text)
WHERE channel_id = $2
  AND slack_ts = $3
`

type TagAsUserMessageParams struct {
	UserID    string
	ChannelID string
	SlackTs   string
}

func (q *Queries) TagAsUserMessage(ctx context.Context, arg TagAsUserMessageParams) error {
	_, err := q.db.Exec(ctx, tagAsUserMessage, arg.UserID, arg.ChannelID, arg.SlackTs)
	return err
}
