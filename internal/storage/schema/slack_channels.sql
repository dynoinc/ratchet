-- name: AddChannel :one
INSERT INTO channels (channel_id)
VALUES ($1)
ON CONFLICT (channel_id) DO UPDATE SET channel_id = EXCLUDED.channel_id
RETURNING *;

-- name: GetSlackChannels :many
SELECT * FROM channels;
