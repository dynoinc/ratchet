// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: channels.sql

package schema

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const addChannel = `-- name: AddChannel :one
INSERT INTO channels (
    channel_id,
    channel_name
) VALUES (
    $1, $2
)
ON CONFLICT (channel_id) DO UPDATE
SET channel_name = EXCLUDED.channel_name
RETURNING channel_id, channel_name, created_at, slack_ts_watermark
`

type AddChannelParams struct {
	ChannelID   string
	ChannelName pgtype.Text
}

func (q *Queries) AddChannel(ctx context.Context, arg AddChannelParams) (Channel, error) {
	row := q.db.QueryRow(ctx, addChannel, arg.ChannelID, arg.ChannelName)
	var i Channel
	err := row.Scan(
		&i.ChannelID,
		&i.ChannelName,
		&i.CreatedAt,
		&i.SlackTsWatermark,
	)
	return i, err
}

const getChannel = `-- name: GetChannel :one
SELECT channel_id, channel_name, created_at, slack_ts_watermark FROM channels
WHERE channel_id = $1
`

func (q *Queries) GetChannel(ctx context.Context, channelID string) (Channel, error) {
	row := q.db.QueryRow(ctx, getChannel, channelID)
	var i Channel
	err := row.Scan(
		&i.ChannelID,
		&i.ChannelName,
		&i.CreatedAt,
		&i.SlackTsWatermark,
	)
	return i, err
}

const getChannelByName = `-- name: GetChannelByName :one
SELECT channel_id, channel_name, created_at, slack_ts_watermark FROM channels
WHERE channel_name = $1
`

func (q *Queries) GetChannelByName(ctx context.Context, channelName pgtype.Text) (Channel, error) {
	row := q.db.QueryRow(ctx, getChannelByName, channelName)
	var i Channel
	err := row.Scan(
		&i.ChannelID,
		&i.ChannelName,
		&i.CreatedAt,
		&i.SlackTsWatermark,
	)
	return i, err
}

const getChannels = `-- name: GetChannels :many
SELECT channel_id, channel_name, created_at, slack_ts_watermark FROM channels
`

func (q *Queries) GetChannels(ctx context.Context) ([]Channel, error) {
	rows, err := q.db.Query(ctx, getChannels)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Channel
	for rows.Next() {
		var i Channel
		if err := rows.Scan(
			&i.ChannelID,
			&i.ChannelName,
			&i.CreatedAt,
			&i.SlackTsWatermark,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const removeChannel = `-- name: RemoveChannel :exec
DELETE FROM channels WHERE channel_id = $1
`

func (q *Queries) RemoveChannel(ctx context.Context, channelID string) error {
	_, err := q.db.Exec(ctx, removeChannel, channelID)
	return err
}

const updateSlackTSWatermark = `-- name: UpdateSlackTSWatermark :exec
UPDATE channels
SET slack_ts_watermark = $2
WHERE channel_id = $1
`

type UpdateSlackTSWatermarkParams struct {
	ChannelID        string
	SlackTsWatermark string
}

func (q *Queries) UpdateSlackTSWatermark(ctx context.Context, arg UpdateSlackTSWatermarkParams) error {
	_, err := q.db.Exec(ctx, updateSlackTSWatermark, arg.ChannelID, arg.SlackTsWatermark)
	return err
}
