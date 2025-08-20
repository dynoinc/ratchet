-- name: AddMessage :one
WITH ins AS (
    INSERT INTO messages_v3 (channel_id, ts, attrs)
    VALUES (@channel_id, @ts, @attrs)
    ON CONFLICT (channel_id, ts) DO NOTHING
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM ins) AS inserted;

-- name: AddThreadMessage :one
WITH ins AS (
    INSERT INTO messages_v3 (channel_id, parent_ts, ts, attrs)
    VALUES (@channel_id, @parent_ts :: text, @ts, @attrs)
    ON CONFLICT (channel_id, ts) DO NOTHING
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM ins) AS inserted;

-- name: UpdateReaction :exec
WITH reaction_count AS (SELECT COALESCE((attrs -> 'reactions' ->> (@reaction::text))::int, 0) + @count::int AS new_count
                        FROM messages_v3
                        WHERE channel_id = @channel_id
                          AND ts = @ts)
UPDATE messages_v3 m
SET attrs = jsonb_set(
        COALESCE(m.attrs, '{}'::jsonb),
        '{reactions}'::text[],
        CASE
            WHEN (SELECT new_count FROM reaction_count) <= 0 THEN
                COALESCE(m.attrs -> 'reactions', '{}'::jsonb) - @reaction::text
            ELSE
                jsonb_set(
                        COALESCE(m.attrs -> 'reactions', '{}'::jsonb),
                        array [@reaction::text],
                        to_jsonb((SELECT new_count FROM reaction_count))
                )
            END
            )
WHERE m.channel_id = @channel_id
  AND m.ts = @ts;

-- name: UpdateMessageAttrs :exec
UPDATE
    messages_v3
SET attrs     = COALESCE(attrs, '{}' :: jsonb) || @attrs,
    embedding = @embedding
WHERE channel_id = @channel_id
  AND ts = @ts;

-- name: GetMessage :one
SELECT channel_id,
       ts,
       attrs,
       embedding
FROM messages_v3
WHERE channel_id = @channel_id
  AND ts = @ts;

-- name: GetAllMessages :many
SELECT channel_id,
       ts,
       attrs
FROM messages_v3
WHERE channel_id = @channel_id
  AND parent_ts IS NULL
ORDER BY (ts::float) DESC
LIMIT @n;

-- name: GetMessagesWithinTS :many
SELECT channel_id,
       ts,
       attrs
FROM messages_v3
WHERE channel_id = @channel_id
  AND ts::float BETWEEN @start_ts
    AND @end_ts;

-- name: GetMessagesByUser :many
SELECT channel_id,
       ts,
       attrs
FROM messages_v3
WHERE ts::float BETWEEN @start_ts AND @end_ts
  AND attrs -> 'message' ->> 'user' = @user_id :: text;

-- name: GetServices :many
SELECT service :: text
FROM (SELECT DISTINCT attrs -> 'incident_action' ->> 'service' as service
      FROM messages_v3
      WHERE attrs -> 'incident_action' ->> 'service' IS NOT NULL
        AND parent_ts IS NULL) s;

-- name: GetAlerts :many
WITH alert_counts AS (SELECT m.attrs -> 'incident_action' ->> 'service'  as service,
                             m.attrs -> 'incident_action' ->> 'alert'    as alert,
                             m.attrs -> 'incident_action' ->> 'priority' as priority,
                             COUNT(t.ts)                                 as thread_message_count
                      FROM messages_v3 m
                               LEFT JOIN messages_v3 t ON
                          t.channel_id = m.channel_id AND
                          t.parent_ts = m.ts
                      WHERE (
                          @service :: text = '*'
                              OR m.attrs -> 'incident_action' ->> 'service' = @service :: text
                          )
                        AND m.attrs -> 'incident_action' ->> 'action' = 'open_incident'
                        AND m.parent_ts IS NULL
                      GROUP BY m.attrs -> 'incident_action' ->> 'service',
                               m.attrs -> 'incident_action' ->> 'alert',
                               m.attrs -> 'incident_action' ->> 'priority')
SELECT service :: text                as service,
       alert :: text                  as alert,
       priority :: text               as priority,
       thread_message_count :: bigint as thread_message_count
FROM alert_counts;

-- name: GetThreadMessages :many
WITH parent_message AS (
    -- Get the parent message
    SELECT m.channel_id,
           m.parent_ts,
           m.ts,
           m.attrs
    FROM messages_v3 m
    WHERE m.channel_id = @channel_id
      AND m.ts = @parent_ts :: text
      AND (@bot_id :: text = '' OR m.attrs -> 'message' ->> 'user' != @bot_id :: text)
),
thread_replies AS (
    -- Get the thread replies
    SELECT m.channel_id,
           m.parent_ts,
           m.ts,
           m.attrs
    FROM messages_v3 m
    WHERE m.channel_id = @channel_id
      AND m.parent_ts = @parent_ts :: text
      AND (@bot_id :: text = '' OR m.attrs -> 'message' ->> 'user' != @bot_id :: text)
    ORDER BY (m.ts::float) DESC
    LIMIT @limit_val
)
SELECT channel_id,
       parent_ts,
       ts,
       attrs
FROM (
    SELECT * FROM parent_message
    UNION ALL
    SELECT * FROM thread_replies
) combined
ORDER BY (ts::float) ASC;

-- name: GetThreadMessagesByServiceAndAlert :many
SELECT t.channel_id,
       t.parent_ts,
       t.ts,
       t.attrs
FROM messages_v3 t
         JOIN messages_v3 m ON m.channel_id = t.channel_id
    AND m.ts = t.parent_ts
WHERE m.attrs -> 'incident_action' ->> 'service' = @service :: text
  AND m.attrs -> 'incident_action' ->> 'alert' = @alert :: text
  AND m.parent_ts IS NULL
  AND t.attrs -> 'message' ->> 'user' != @bot_id :: text;


-- name: DeleteOldMessages :exec
DELETE
FROM messages_v3
WHERE channel_id = @channel_id
  AND CAST(ts AS numeric) < EXTRACT(
        epoch
        FROM
        NOW() - @older_than :: interval
                            );

-- name: GetLatestServiceUpdates :many
WITH valid_messages AS (SELECT channel_id,
                               ts,
                               attrs,
                               embedding,
                               CASE
                                   WHEN attrs -> 'message' ->> 'text' = '' OR attrs -> 'message' ->> 'text' IS NULL
                                       THEN -1
                                   ELSE ts_rank(tsvec, plainto_tsquery('english', @query_text :: text))
                                   END as lexical_score
                        FROM messages_v3
                        WHERE CAST(SPLIT_PART(ts, '.', 1) AS numeric) > EXTRACT(
                                epoch
                                FROM
                                NOW() - @interval :: interval
                                                                        )
                          AND attrs -> 'message' ->> 'user' != @bot_id :: text
                          AND attrs -> 'incident_action' ->> 'action' IS NULL),
     semantic_matches AS (SELECT channel_id,
                                 ts,
                                 ROW_NUMBER() OVER (
                                     ORDER BY
                                         embedding <=> @query_embedding
                                     ) as semantic_rank
                          FROM valid_messages),
     lexical_matches AS (SELECT channel_id,
                                ts,
                                ROW_NUMBER() OVER (
                                    ORDER BY
                                        lexical_score DESC
                                    ) as lexical_rank
                         FROM valid_messages),
     combined_scores AS (SELECT s.channel_id :: text                       as channel_id,
                                s.ts :: text                               as ts,
                                COALESCE(s.semantic_rank, 1000)            as semantic_rank,
                                COALESCE(l.lexical_rank, 1000)             as lexical_rank,
                                -- Reciprocal Rank Fusion with k=1 for small result sets
                                1.0 / (1 + COALESCE(s.semantic_rank, 1000)) +
                                1.0 / (1 + COALESCE(l.lexical_rank, 1000)) as rrf_score
                         FROM semantic_matches s
                                  FULL OUTER JOIN lexical_matches l ON s.channel_id = l.channel_id AND s.ts = l.ts)
SELECT m.channel_id,
       m.ts,
       m.attrs,
       c.semantic_rank,
       c.lexical_rank,
       c.rrf_score :: float
FROM valid_messages m
         INNER JOIN combined_scores c ON m.channel_id = c.channel_id AND m.ts = c.ts
ORDER BY c.rrf_score DESC
LIMIT 5;

-- name: DebugGetLatestServiceUpdates :many
WITH valid_messages AS (SELECT channel_id,
                               ts,
                               embedding,
                               CASE
                                   WHEN attrs -> 'message' ->> 'text' = '' OR attrs -> 'message' ->> 'text' IS NULL
                                       THEN -1
                                   ELSE ts_rank(tsvec, plainto_tsquery('english', @query_text :: text))
                                   END                                         as lexical_score,
                               -- Include the text and tokens for debugging
                               attrs -> 'message' ->> 'text'                   as message_text,
                               tsvec                                           as text_tokens,
                               plainto_tsquery('english', @query_text :: text) as query_tokens
                        FROM messages_v3
                        WHERE CAST(SPLIT_PART(ts, '.', 1) AS numeric) > EXTRACT(
                                epoch
                                FROM
                                NOW() - @interval :: interval
                                                                        )
                          AND attrs -> 'message' ->> 'user' != @bot_id :: text
                          AND attrs -> 'incident_action' ->> 'action' IS NULL),
     semantic_matches AS (SELECT channel_id,
                                 ts,
                                 embedding <=> @query_embedding as semantic_distance,
                                 ROW_NUMBER() OVER (
                                     ORDER BY
                                         embedding <=> @query_embedding
                                     )                          as semantic_rank
                          FROM valid_messages),
     lexical_matches AS (SELECT channel_id,
                                ts,
                                ROW_NUMBER() OVER (
                                    ORDER BY
                                        lexical_score DESC
                                    ) as lexical_rank
                         FROM valid_messages),
     results AS (SELECT m.channel_id,
                        m.ts,
                        COALESCE(s.semantic_rank, 1000)                                                          as semantic_rank,
                        COALESCE(s.semantic_distance, 2.0)                                                       as semantic_distance,
                        COALESCE(l.lexical_rank, 1000)                                                           as lexical_rank,
                        1.0 / (1 + COALESCE(s.semantic_rank, 1000)) + 1.0 /
                                                                      (1 + COALESCE(l.lexical_rank, 1000))       as rrf_score,
                        COALESCE(m.message_text, 'NULL')                                                         as message_text,
                        COALESCE(m.text_tokens, 'NULL')                                                          as text_tokens,
                        COALESCE(m.query_tokens, 'NULL')                                                         as query_tokens,
                        m.lexical_score
                 FROM valid_messages m
                          LEFT JOIN semantic_matches s ON m.channel_id = s.channel_id AND m.ts = s.ts
                          LEFT JOIN lexical_matches l ON m.channel_id = l.channel_id AND m.ts = l.ts)
SELECT channel_id,
       ts,
       semantic_rank,
       semantic_distance::float as semantic_distance,
       lexical_rank,
       rrf_score::float         as rrf_score,
       message_text::text       as message_text,
       text_tokens::text        as text_tokens,
       query_tokens::text       as query_tokens,
       lexical_score::float     as lexical_score
FROM results
ORDER BY rrf_score DESC;

-- name: SearchMessagesHybrid :many
WITH channel_filter AS (
    SELECT id FROM channels_v2 
    WHERE (@channel_names::text[] IS NULL 
           OR attrs ->> 'name' = ANY(@channel_names::text[]))
),
valid_messages AS (
    SELECT m.channel_id,
           m.ts,
           m.parent_ts,
           m.attrs,
           m.embedding,
           c.attrs ->> 'name' as channel_name,
           CASE
               WHEN m.attrs -> 'message' ->> 'text' = '' OR m.attrs -> 'message' ->> 'text' IS NULL
                   THEN -1
               ELSE ts_rank(m.tsvec, plainto_tsquery('english', @query_text :: text))
               END as lexical_score
    FROM messages_v3 m
    JOIN channels_v2 c ON m.channel_id = c.id
    LEFT JOIN channel_filter cf ON m.channel_id = cf.id
    WHERE (@channel_names::text[] IS NULL OR cf.id IS NOT NULL)
      AND (@bot_id :: text = '' OR m.attrs -> 'message' ->> 'user' != @bot_id :: text)
      AND m.attrs -> 'incident_action' ->> 'action' IS NULL
),
semantic_matches AS (
    SELECT channel_id,
           ts,
           ROW_NUMBER() OVER (
               ORDER BY
                   embedding <=> @query_embedding
               ) as semantic_rank
    FROM valid_messages
    WHERE embedding IS NOT NULL
),
lexical_matches AS (
    SELECT channel_id,
           ts,
           ROW_NUMBER() OVER (
               ORDER BY
                   lexical_score DESC
               ) as lexical_rank
    FROM valid_messages
    WHERE lexical_score > 0
),
combined_scores AS (
    SELECT s.channel_id :: text                       as channel_id,
           s.ts :: text                               as ts,
           COALESCE(s.semantic_rank, 1000)            as semantic_rank,
           COALESCE(l.lexical_rank, 1000)             as lexical_rank,
           -- Reciprocal Rank Fusion with k=1 for small result sets
           1.0 / (1 + COALESCE(s.semantic_rank, 1000)) +
           1.0 / (1 + COALESCE(l.lexical_rank, 1000)) as rrf_score
    FROM semantic_matches s
             FULL OUTER JOIN lexical_matches l ON s.channel_id = l.channel_id AND s.ts = l.ts
)
SELECT m.channel_id,
       m.ts,
       m.parent_ts,
       m.attrs,
       m.channel_name,
       c.semantic_rank,
       c.lexical_rank,
       c.rrf_score :: float
FROM valid_messages m
         INNER JOIN combined_scores c ON m.channel_id = c.channel_id AND m.ts = c.ts
ORDER BY c.rrf_score DESC
LIMIT @limit_val;
