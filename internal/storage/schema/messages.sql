-- name: AddMessage :exec
INSERT INTO
    messages_v2 (channel_id, ts, attrs)
VALUES
    (@channel_id, @ts, @attrs) ON CONFLICT (channel_id, ts) DO NOTHING;

-- name: UpdateMessageAttrs :exec
UPDATE
    messages_v2
SET
    attrs = COALESCE(attrs, '{}' :: jsonb) || @attrs,
    embedding = @embedding
WHERE
    channel_id = @channel_id
    AND ts = @ts;

-- name: GetMessage :one
SELECT
    channel_id,
    ts,
    attrs,
    embedding
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
    service :: text,
    alert :: text,
    priority :: text
FROM
    (
        SELECT
            DISTINCT attrs -> 'incident_action' ->> 'service' as service,
            attrs -> 'incident_action' ->> 'alert' as alert,
            attrs -> 'incident_action' ->> 'priority' as priority
        FROM
            messages_v2
        WHERE
            (
                @service :: text = '*'
                OR attrs -> 'incident_action' ->> 'service' = @service :: text
            )
            AND attrs -> 'incident_action' ->> 'action' = 'open_incident'
    ) subq;

-- name: DeleteOldMessages :exec
DELETE FROM
    messages_v2
WHERE
    channel_id = @channel_id
    AND CAST(ts AS numeric) < EXTRACT(
        epoch
        FROM
            NOW() - @older_than :: interval
    );

-- name: GetLatestServiceUpdates :many
WITH semantic_matches AS (
    SELECT
        channel_id,
        ts,
        ROW_NUMBER() OVER (
            ORDER BY
                messages_v2.embedding <=> @query_embedding
        ) as semantic_rank
    FROM
        messages_v2
    WHERE
        messages_v2.attrs -> 'incident_action' ->> 'service' = @service_name :: text
        AND CAST(ts AS numeric) > EXTRACT(
            epoch
            FROM
                NOW() - @interval :: interval
        )
),
lexical_matches AS (
    SELECT
        channel_id,
        ts,
        ROW_NUMBER() OVER (
            ORDER BY
                ts_rank_cd(to_tsvector('english', attrs -> 'message' ->> 'text'), 
                          plainto_tsquery('english', @query_text :: text)) DESC
        ) as lexical_rank
    FROM
        messages_v2
    WHERE
        attrs -> 'incident_action' ->> 'service' = @service_name :: text
        AND CAST(ts AS numeric) > EXTRACT(
            epoch
            FROM
                NOW() - @interval :: interval
        )
),
combined_scores AS (
    SELECT
        s.channel_id :: text as channel_id,
        s.ts :: text as ts,
        0.4 / (60.0 + COALESCE(s.semantic_rank, 1000)) + 0.6 / (60.0 + COALESCE(l.lexical_rank, 1000)) as rrf_score
    FROM
        semantic_matches s FULL
        OUTER JOIN lexical_matches l ON s.channel_id = l.channel_id AND s.ts = l.ts
),
top_matches AS (
    SELECT
        channel_id,
        ts
    FROM
        combined_scores
    ORDER BY
        rrf_score DESC
    LIMIT
        10
)
SELECT
    m.channel_id,
    m.ts,
    m.attrs,
    m.embedding
FROM
    messages_v2 m
    INNER JOIN top_matches t ON m.channel_id = t.channel_id :: text
    AND m.ts = t.ts :: text;