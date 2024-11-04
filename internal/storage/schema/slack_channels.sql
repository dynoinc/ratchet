-- name: InsertOrEnableChannel :one
INSERT INTO channels (channel_id, enabled)
VALUES ($1, TRUE)
ON CONFLICT (channel_id) DO UPDATE SET enabled = TRUE
RETURNING *;

-- name: DisableSlackChannel :one
UPDATE channels
SET enabled = FALSE
WHERE channel_id = $1
RETURNING *;

-- name: GetSlackChannels :many
SELECT * FROM channels;