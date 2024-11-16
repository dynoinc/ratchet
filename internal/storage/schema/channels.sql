-- name: AddChannel :one
INSERT INTO channels (channel_id)
VALUES ($1)
ON CONFLICT (channel_id) DO UPDATE SET channel_id = channels.channel_id
RETURNING *;

-- name: UpdateLatestSlackTs :exec
UPDATE channels
SET latest_slack_ts = $2
WHERE channel_id = $1;

-- name: GetChannel :one
SELECT * FROM channels
WHERE channel_id = $1;

-- name: GetChannels :many
SELECT * FROM channels;

-- name: RemoveChannel :exec
DELETE FROM channels WHERE channel_id = $1;
