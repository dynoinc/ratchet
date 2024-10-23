// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: query.sql

package schema

import (
	"context"
)

const getSlackChannelByID = `-- name: GetSlackChannelByID :one
SELECT channel_id, team_name, added_ts FROM slack_channels
WHERE channel_id = $1
`

func (q *Queries) GetSlackChannelByID(ctx context.Context, channelID string) (SlackChannel, error) {
	row := q.db.QueryRow(ctx, getSlackChannelByID, channelID)
	var i SlackChannel
	err := row.Scan(&i.ChannelID, &i.TeamName, &i.AddedTs)
	return i, err
}

const getSlackChannelsByTeam = `-- name: GetSlackChannelsByTeam :many
SELECT channel_id, team_name, added_ts FROM slack_channels
WHERE team_name = $1
ORDER BY added_ts DESC
`

func (q *Queries) GetSlackChannelsByTeam(ctx context.Context, teamName string) ([]SlackChannel, error) {
	rows, err := q.db.Query(ctx, getSlackChannelsByTeam, teamName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []SlackChannel
	for rows.Next() {
		var i SlackChannel
		if err := rows.Scan(&i.ChannelID, &i.TeamName, &i.AddedTs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const insertSlackChannel = `-- name: InsertSlackChannel :one
INSERT INTO slack_channels (
    channel_id,
    team_name,
    added_ts
) VALUES (
    $1,
    $2,
    COALESCE($3, CURRENT_TIMESTAMP))
RETURNING channel_id, team_name, added_ts
`

type InsertSlackChannelParams struct {
	ChannelID string
	TeamName  string
	Column3   interface{}
}

func (q *Queries) InsertSlackChannel(ctx context.Context, arg InsertSlackChannelParams) (SlackChannel, error) {
	row := q.db.QueryRow(ctx, insertSlackChannel, arg.ChannelID, arg.TeamName, arg.Column3)
	var i SlackChannel
	err := row.Scan(&i.ChannelID, &i.TeamName, &i.AddedTs)
	return i, err
}