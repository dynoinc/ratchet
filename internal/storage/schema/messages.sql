-- name: AddMessage :exec
INSERT INTO
    messages_v2 (channel_id, ts, attrs)
VALUES
    (@channel_id, @ts, @attrs) ON CONFLICT (channel_id, ts) DO NOTHING;

-- name: UpdateMessageAttrs :exec
UPDATE
    messages_v2
SET
    attrs = COALESCE(attrs, '{}' :: jsonb) || @attrs
WHERE
    channel_id = @channel_id
    AND ts = @ts;

-- name: GetMessage :one
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = @channel_id
    AND ts = @ts;

-- name: GetAllMessages :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = @channel_id;

-- name: GetMessagesWithinTS :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = @channel_id
    AND ts BETWEEN @start_ts
    AND @end_ts;