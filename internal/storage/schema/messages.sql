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

-- name: GetAllOpenIncidentMessages :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    channel_id = @channel_id
    AND attrs -> 'incident_action' ->> 'action' = 'open_incident'
    AND attrs -> 'incident_action' ->> 'service' = @service :: text
    AND attrs -> 'incident_action' ->> 'alert' = @alert :: text
ORDER BY
    CAST(ts AS numeric) ASC;

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

-- name: GetServices :many
SELECT
    service :: text
FROM
    (
        SELECT
            DISTINCT attrs -> 'incident_action' ->> 'service' as service
        FROM
            messages_v2
        WHERE
            attrs -> 'incident_action' ->> 'service' IS NOT NULL
    ) s;

-- name: GetAlerts :many
SELECT
    alert :: text,
    priority :: text
FROM
    (
        SELECT
            DISTINCT attrs -> 'incident_action' ->> 'alert' as alert,
            attrs -> 'incident_action' ->> 'priority' as priority
        FROM
            messages_v2
        WHERE
            attrs -> 'incident_action' ->> 'service' = @service :: text
            AND attrs -> 'incident_action' ->> 'action' = 'open_incident'
    ) subq;

-- name: GetLatestServiceUpdates :many
SELECT
    channel_id,
    ts,
    attrs
FROM
    messages_v2
WHERE
    attrs -> 'ai_classification' ->> 'service' = @service :: text
    AND CAST(ts AS numeric) > EXTRACT(
        epoch
        FROM
            NOW() - INTERVAL '5 minutes'
    )
ORDER BY
    CAST(ts AS numeric) DESC
LIMIT
    5;