-- name: AddChannel :one
INSERT INTO channels (channel_id)
VALUES ($1)
ON CONFLICT (channel_id) DO UPDATE
    SET channel_id = channels.channel_id 
RETURNING *;

-- name: GetChannel :one
SELECT *
FROM channels
WHERE channel_id = $1;

-- name: GetChannelByName :one
SELECT *
FROM channels
WHERE attrs ->> 'name' = $1::text;

-- name: GetChannels :many
SELECT *
FROM channels;

-- name: RemoveChannel :exec
DELETE
FROM channels
WHERE channel_id = $1;

-- name: UpdateChannelAttrs :exec
UPDATE channels
SET attrs = channels.attrs || $2
WHERE channel_id = $1;

-- name: UpdateChannelSlackTSWatermark :exec
UPDATE channels
SET slack_ts_watermark = $2
WHERE channel_id = $1;
