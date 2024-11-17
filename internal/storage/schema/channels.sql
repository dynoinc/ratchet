-- name: AddChannel :one
INSERT INTO channels (
    channel_id,
    channel_name
) VALUES (
    $1, $2
)
ON CONFLICT (channel_id) DO UPDATE
SET channel_name = EXCLUDED.channel_name
RETURNING *;

-- name: GetChannel :one
SELECT * FROM channels
WHERE channel_id = $1;

-- name: GetChannelByName :one
SELECT * FROM channels
WHERE channel_name = $1;

-- name: GetChannels :many
SELECT * FROM channels;

-- name: RemoveChannel :exec
DELETE FROM channels WHERE channel_id = $1;

-- name: UpdateSlackTSWatermark :exec
UPDATE channels
SET slack_ts_watermark = $2
WHERE channel_id = $1;
