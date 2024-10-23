-- name: InsertSlackChannel :one
INSERT INTO slack_channels (
    channel_id,
    team_name,
    added_ts
) VALUES (
    $1,
    $2,
    COALESCE($3, CURRENT_TIMESTAMP))
RETURNING *;

-- name: GetSlackChannelByID :one
SELECT * FROM slack_channels
WHERE channel_id = $1;

-- name: GetSlackChannelsByTeam :many
SELECT * FROM slack_channels
WHERE team_name = $1
ORDER BY added_ts DESC;