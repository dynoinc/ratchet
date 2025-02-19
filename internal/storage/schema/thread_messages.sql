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

-- name: GetThreadMessagesByServiceAndAlert :many
SELECT
    t.channel_id,
    t.parent_ts,
    t.ts,
    t.attrs
FROM
    thread_messages_v2 t
    JOIN messages_v2 m ON m.channel_id = t.channel_id
    AND m.ts = t.parent_ts
WHERE
    m.attrs -> 'incident_action' ->> 'service' = @service :: text
    AND m.attrs -> 'incident_action' ->> 'alert' = @alert :: text
    AND attrs -> 'message' ->> 'user' != @bot_id :: text;