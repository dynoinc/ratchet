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
    priority :: text,
    COUNT(t.ts) as thread_message_count
FROM
    (
        SELECT
            DISTINCT m.channel_id,
            m.ts,
            attrs -> 'incident_action' ->> 'service' as service,
            attrs -> 'incident_action' ->> 'alert' as alert,
            attrs -> 'incident_action' ->> 'priority' as priority
        FROM
            messages_v2 m
        WHERE
            (
                @service :: text = '*'
                OR attrs -> 'incident_action' ->> 'service' = @service :: text
            )
            AND attrs -> 'incident_action' ->> 'action' = 'open_incident'
    ) subq
    LEFT JOIN thread_messages_v2 t ON t.channel_id = subq.channel_id AND t.parent_ts = subq.ts
GROUP BY
    service,
    alert,
    priority;

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
WITH valid_messages AS (
    SELECT
        channel_id,
        ts,
        attrs,
        embedding,
        ts_rank_cd(to_tsvector('english', attrs -> 'message' ->> 'text'),
                   plainto_tsquery('english', @query_text :: text)) as lexical_score
    FROM
        messages_v2
    WHERE
        CAST(ts AS numeric) > EXTRACT(
            epoch
            FROM
                NOW() - @interval :: interval
        )
        AND attrs -> 'message' ->> 'user' != @bot_id :: text
        AND attrs -> 'incident_action' ->> 'action' IS NULL 
),
semantic_matches AS (
    SELECT
        channel_id,
        ts,
        ROW_NUMBER() OVER (
            ORDER BY
                embedding <=> @query_embedding
        ) as semantic_rank
    FROM
        valid_messages
),
lexical_matches AS (
    SELECT
        channel_id,
        ts,
        ROW_NUMBER() OVER (
            ORDER BY
                lexical_score DESC
        ) as lexical_rank
    FROM
        valid_messages
),
combined_scores AS (
    SELECT
        s.channel_id :: text as channel_id,
        s.ts :: text as ts,
        s.semantic_rank,
        l.lexical_rank,
        -- Reciprocal Rank Fusion with k=1 for small result sets
        1.0 / (1 + COALESCE(s.semantic_rank, 1000)) + 1.0 / (1 + COALESCE(l.lexical_rank, 1000)) as rrf_score
    FROM
        semantic_matches s
        FULL OUTER JOIN lexical_matches l ON s.channel_id = l.channel_id AND s.ts = l.ts
    -- Require at least one match type to have a reasonable rank
    WHERE s.semantic_rank <= 10 OR l.lexical_rank <= 10
)
SELECT
    m.channel_id,
    m.ts,
    m.attrs,
    c.semantic_rank,
    c.lexical_rank,
    c.rrf_score :: float
FROM
    valid_messages m
    INNER JOIN combined_scores c ON m.channel_id = c.channel_id AND m.ts = c.ts
ORDER BY
    c.rrf_score DESC
LIMIT 5;