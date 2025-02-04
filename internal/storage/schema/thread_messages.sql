-- name: AddThreadMessage :exec
INSERT INTO
    thread_messages_v2 (channel_id, parent_ts, ts, attrs)
VALUES
    (@channel_id, @parent_ts, @ts, @attrs) ON CONFLICT (channel_id, parent_ts, ts) DO NOTHING;

-- name: GetThreadMessages :many
SELECT
    channel_id,
    parent_ts,
    ts,
    attrs
FROM
    thread_messages_v2
WHERE
    channel_id = @channel_id
    AND parent_ts = @parent_ts;