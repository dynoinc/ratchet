// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.28.0
// source: messages.sql

package schema

import (
	"context"

	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

const addMessage = `-- name: AddMessage :exec
INSERT INTO
    messages_v2 (channel_id, ts, attrs)
VALUES
    ($1, $2, $3) ON CONFLICT (channel_id, ts) DO NOTHING
`

type AddMessageParams struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) AddMessage(ctx context.Context, arg AddMessageParams) error {
	_, err := q.db.Exec(ctx, addMessage, arg.ChannelID, arg.Ts, arg.Attrs)
	return err
}

const getAlerts = `-- name: GetAlerts :many
SELECT
    service :: text,
    alert :: text,
    priority :: text
FROM
    (
        SELECT
            DISTINCT attrs -> 'incident_action' ->> 'service' as service,
            attrs -> 'incident_action' ->> 'alert' as alert,
            attrs -> 'incident_action' ->> 'priority' as priority
        FROM
            messages_v2
        WHERE
            (
                $1 :: text = '*'
                OR attrs -> 'incident_action' ->> 'service' = $1 :: text
            )
            AND attrs -> 'incident_action' ->> 'action' = 'open_incident'
    ) subq
`

type GetAlertsRow struct {
	Service  string
	Alert    string
	Priority string
}

func (q *Queries) GetAlerts(ctx context.Context, service string) ([]GetAlertsRow, error) {
	rows, err := q.db.Query(ctx, getAlerts, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetAlertsRow
	for rows.Next() {
		var i GetAlertsRow
		if err := rows.Scan(&i.Service, &i.Alert, &i.Priority); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getAllMessages = `-- name: GetAllMessages :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = $1
`

func (q *Queries) GetAllMessages(ctx context.Context, channelID string) ([]MessagesV2, error) {
	rows, err := q.db.Query(ctx, getAllMessages, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MessagesV2
	for rows.Next() {
		var i MessagesV2
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getAllOpenIncidentMessages = `-- name: GetAllOpenIncidentMessages :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = $1
    AND attrs -> 'incident_action' ->> 'action' = 'open_incident'
    AND attrs -> 'incident_action' ->> 'service' = $2 :: text
    AND attrs -> 'incident_action' ->> 'alert' = $3 :: text
ORDER BY
    CAST(ts AS numeric) ASC
`

type GetAllOpenIncidentMessagesParams struct {
	ChannelID string
	Service   string
	Alert     string
}

func (q *Queries) GetAllOpenIncidentMessages(ctx context.Context, arg GetAllOpenIncidentMessagesParams) ([]MessagesV2, error) {
	rows, err := q.db.Query(ctx, getAllOpenIncidentMessages, arg.ChannelID, arg.Service, arg.Alert)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MessagesV2
	for rows.Next() {
		var i MessagesV2
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getLatestServiceUpdates = `-- name: GetLatestServiceUpdates :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    attrs -> 'ai_classification' ->> 'service' = $1 :: text
    AND CAST(ts AS numeric) > EXTRACT(
        epoch
        FROM
            NOW() - INTERVAL '5 minutes'
    )
ORDER BY
    CAST(ts AS numeric) DESC
LIMIT
    5
`

func (q *Queries) GetLatestServiceUpdates(ctx context.Context, service string) ([]MessagesV2, error) {
	rows, err := q.db.Query(ctx, getLatestServiceUpdates, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MessagesV2
	for rows.Next() {
		var i MessagesV2
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
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
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = $1
    AND ts = $2
`

type GetMessageParams struct {
	ChannelID string
	Ts        string
}

func (q *Queries) GetMessage(ctx context.Context, arg GetMessageParams) (MessagesV2, error) {
	row := q.db.QueryRow(ctx, getMessage, arg.ChannelID, arg.Ts)
	var i MessagesV2
	err := row.Scan(&i.ChannelID, &i.Ts, &i.Attrs)
	return i, err
}

const getMessagesWithinTS = `-- name: GetMessagesWithinTS :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = $1
    AND ts BETWEEN $2
    AND $3
`

type GetMessagesWithinTSParams struct {
	ChannelID string
	StartTs   string
	EndTs     string
}

func (q *Queries) GetMessagesWithinTS(ctx context.Context, arg GetMessagesWithinTSParams) ([]MessagesV2, error) {
	rows, err := q.db.Query(ctx, getMessagesWithinTS, arg.ChannelID, arg.StartTs, arg.EndTs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MessagesV2
	for rows.Next() {
		var i MessagesV2
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getServices = `-- name: GetServices :many
SELECT
    service :: text
FROM
    (
        SELECT
            DISTINCT attrs -> 'incident_action' ->> 'service' as service
        FROM
            messages_v2
        WHERE
            attrs -> 'incident_action' ->> 'service' IS NOT NULL
    ) s
`

func (q *Queries) GetServices(ctx context.Context) ([]string, error) {
	rows, err := q.db.Query(ctx, getServices)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var service string
		if err := rows.Scan(&service); err != nil {
			return nil, err
		}
		items = append(items, service)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateMessageAttrs = `-- name: UpdateMessageAttrs :exec
UPDATE
    messages_v2
SET
    attrs = COALESCE(attrs, '{}' :: jsonb) || $1
WHERE
    channel_id = $2
    AND ts = $3
`

type UpdateMessageAttrsParams struct {
	Attrs     dto.MessageAttrs
	ChannelID string
	Ts        string
}

func (q *Queries) UpdateMessageAttrs(ctx context.Context, arg UpdateMessageAttrsParams) error {
	_, err := q.db.Exec(ctx, updateMessageAttrs, arg.Attrs, arg.ChannelID, arg.Ts)
	return err
}
