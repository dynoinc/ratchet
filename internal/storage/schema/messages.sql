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
        CASE 
            WHEN attrs -> 'message' ->> 'text' = '' OR attrs -> 'message' ->> 'text' IS NULL THEN -1
            ELSE ts_rank(to_tsvector('english', attrs -> 'message' ->> 'text'),
                          plainto_tsquery('english', @query_text :: text))
        END as lexical_score
    FROM
        messages_v2
    WHERE
        CAST(SPLIT_PART(ts, '.', 1) AS numeric) > EXTRACT(
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
        COALESCE(s.semantic_rank, 1000) as semantic_rank,
        COALESCE(l.lexical_rank, 1000) as lexical_rank,
        -- Reciprocal Rank Fusion with k=1 for small result sets
        1.0 / (1 + COALESCE(s.semantic_rank, 1000)) + 1.0 / (1 + COALESCE(l.lexical_rank, 1000)) as rrf_score
    FROM
        semantic_matches s
        FULL OUTER JOIN lexical_matches l ON s.channel_id = l.channel_id AND s.ts = l.ts
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

-- name: DebugGetLatestServiceUpdates :many
WITH valid_messages AS (
    SELECT
        channel_id,
        ts,
        embedding,
        CASE 
            WHEN attrs -> 'message' ->> 'text' = '' OR attrs -> 'message' ->> 'text' IS NULL THEN -1
            ELSE ts_rank(to_tsvector('english', attrs -> 'message' ->> 'text'),
                          plainto_tsquery('english', @query_text :: text))
        END as lexical_score,
        -- Include the text and tokens for debugging
        attrs -> 'message' ->> 'text' as message_text,
        to_tsvector('english', attrs -> 'message' ->> 'text') as text_tokens,
        plainto_tsquery('english', @query_text :: text) as query_tokens
    FROM
        messages_v2
    WHERE
        CAST(SPLIT_PART(ts, '.', 1) AS numeric) > EXTRACT(
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
        embedding <=> @query_embedding as semantic_distance,
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
results AS (
    SELECT
        m.channel_id,
        m.ts,
        COALESCE(s.semantic_rank, 1000) as semantic_rank,
        COALESCE(s.semantic_distance, 2.0) as semantic_distance,
        COALESCE(l.lexical_rank, 1000) as lexical_rank,
        1.0 / (1 + COALESCE(s.semantic_rank, 1000)) + 1.0 / (1 + COALESCE(l.lexical_rank, 1000)) as rrf_score,
        COALESCE(m.message_text, 'NULL') as message_text,
        COALESCE(m.text_tokens, 'NULL') as text_tokens,
        COALESCE(m.query_tokens, 'NULL') as query_tokens,
        m.lexical_score
    FROM
        valid_messages m
        LEFT JOIN semantic_matches s ON m.channel_id = s.channel_id AND m.ts = s.ts
        LEFT JOIN lexical_matches l ON m.channel_id = l.channel_id AND m.ts = l.ts
)
SELECT 
    channel_id,
    ts,
    semantic_rank,
    semantic_distance::float as semantic_distance,
    lexical_rank,
    rrf_score::float as rrf_score,
    message_text::text as message_text,
    text_tokens::text as text_tokens,
    query_tokens::text as query_tokens,
    lexical_score::float as lexical_score
FROM results
ORDER BY
    rrf_score DESC;