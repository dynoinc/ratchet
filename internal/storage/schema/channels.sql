-- name: AddChannel :one
INSERT INTO channels (channel_id,
                      attrs)
VALUES ($1, $2)
ON CONFLICT (channel_id) DO UPDATE
    SET attrs = CASE
                    WHEN EXCLUDED.attrs IS NULL OR EXCLUDED.attrs = '{}'::jsonb
                        THEN channels.attrs
                    ELSE EXCLUDED.attrs
        END
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

-- name: UpdateSlackTSWatermark :exec
UPDATE channels
SET slack_ts_watermark = $2
WHERE channel_id = $1;
