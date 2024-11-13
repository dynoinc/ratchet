-- name: AddChannel :exec
INSERT INTO channels (channel_id)
VALUES ($1)
ON CONFLICT DO NOTHING;

-- name: UpdateLatestSlackTs :exec
UPDATE channels
SET latest_slack_ts = $2
WHERE channel_id = $1;

-- name: GetChannel :one
SELECT * FROM channels
WHERE channel_id = $1;

-- name: GetChannels :many
SELECT * FROM channels;
