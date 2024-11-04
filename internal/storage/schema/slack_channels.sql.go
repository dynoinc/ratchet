// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: slack_channels.sql

package schema

import (
	"context"
)

const disableSlackChannel = `-- name: DisableSlackChannel :one
UPDATE channels
SET enabled = FALSE
WHERE channel_id = $1
RETURNING channel_id, enabled, created_at
`

func (q *Queries) DisableSlackChannel(ctx context.Context, channelID string) (Channel, error) {
	row := q.db.QueryRow(ctx, disableSlackChannel, channelID)
	var i Channel
	err := row.Scan(&i.ChannelID, &i.Enabled, &i.CreatedAt)
	return i, err
}

const getSlackChannels = `-- name: GetSlackChannels :many
SELECT channel_id, enabled, created_at FROM channels
`

func (q *Queries) GetSlackChannels(ctx context.Context) ([]Channel, error) {
	rows, err := q.db.Query(ctx, getSlackChannels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Channel
	for rows.Next() {
		var i Channel
		if err := rows.Scan(&i.ChannelID, &i.Enabled, &i.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const insertOrEnableChannel = `-- name: InsertOrEnableChannel :one
INSERT INTO channels (channel_id, enabled)
VALUES ($1, TRUE)
ON CONFLICT (channel_id) DO UPDATE SET enabled = TRUE
RETURNING channel_id, enabled, created_at
`

func (q *Queries) InsertOrEnableChannel(ctx context.Context, channelID string) (Channel, error) {
	row := q.db.QueryRow(ctx, insertOrEnableChannel, channelID)
	var i Channel
	err := row.Scan(&i.ChannelID, &i.Enabled, &i.CreatedAt)
	return i, err
}