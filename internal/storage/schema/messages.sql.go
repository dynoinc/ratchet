// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: messages.sql

package schema

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"

	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

const addMessage = `-- name: AddMessage :one
WITH ins AS (
    INSERT INTO messages_v3 (channel_id, ts, attrs)
    VALUES ($1, $2, $3)
    ON CONFLICT (channel_id, ts) DO NOTHING
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM ins) AS inserted
`

type AddMessageParams struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) AddMessage(ctx context.Context, arg AddMessageParams) (bool, error) {
	row := q.db.QueryRow(ctx, addMessage, arg.ChannelID, arg.Ts, arg.Attrs)
	var inserted bool
	err := row.Scan(&inserted)
	return inserted, err
}

const addThreadMessage = `-- name: AddThreadMessage :one
WITH ins AS (
    INSERT INTO messages_v3 (channel_id, parent_ts, ts, attrs)
    VALUES ($1, $2 :: text, $3, $4)
    ON CONFLICT (channel_id, ts) DO NOTHING
    RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM ins) AS inserted
`

type AddThreadMessageParams struct {
	ChannelID string
	ParentTs  string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) AddThreadMessage(ctx context.Context, arg AddThreadMessageParams) (bool, error) {
	row := q.db.QueryRow(ctx, addThreadMessage,
		arg.ChannelID,
		arg.ParentTs,
		arg.Ts,
		arg.Attrs,
	)
	var inserted bool
	err := row.Scan(&inserted)
	return inserted, err
}

const debugGetLatestServiceUpdates = `-- name: DebugGetLatestServiceUpdates :many
WITH valid_messages AS (SELECT channel_id,
                               ts,
                               embedding,
                               CASE
                                   WHEN attrs -> 'message' ->> 'text' = '' OR attrs -> 'message' ->> 'text' IS NULL
                                       THEN -1
                                   ELSE ts_rank(tsvec, plainto_tsquery('english', $1 :: text))
                                   END                                         as lexical_score,
                               -- Include the text and tokens for debugging
                               attrs -> 'message' ->> 'text'                   as message_text,
                               tsvec                                           as text_tokens,
                               plainto_tsquery('english', $1 :: text) as query_tokens
                        FROM messages_v3
                        WHERE CAST(SPLIT_PART(ts, '.', 1) AS numeric) > EXTRACT(
                                epoch
                                FROM
                                NOW() - $2 :: interval
                                                                        )
                          AND attrs -> 'message' ->> 'user' != $3 :: text
                          AND attrs -> 'incident_action' ->> 'action' IS NULL),
     semantic_matches AS (SELECT channel_id,
                                 ts,
                                 embedding <=> $4 as semantic_distance,
                                 ROW_NUMBER() OVER (
                                     ORDER BY
                                         embedding <=> $4
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
ORDER BY rrf_score DESC
`

type DebugGetLatestServiceUpdatesParams struct {
	QueryText      string
	Interval       pgtype.Interval
	BotID          string
	QueryEmbedding *pgvector.Vector
}

type DebugGetLatestServiceUpdatesRow struct {
	ChannelID        string
	Ts               string
	SemanticRank     int64
	SemanticDistance float64
	LexicalRank      int64
	RrfScore         float64
	MessageText      string
	TextTokens       string
	QueryTokens      string
	LexicalScore     float64
}

func (q *Queries) DebugGetLatestServiceUpdates(ctx context.Context, arg DebugGetLatestServiceUpdatesParams) ([]DebugGetLatestServiceUpdatesRow, error) {
	rows, err := q.db.Query(ctx, debugGetLatestServiceUpdates,
		arg.QueryText,
		arg.Interval,
		arg.BotID,
		arg.QueryEmbedding,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []DebugGetLatestServiceUpdatesRow
	for rows.Next() {
		var i DebugGetLatestServiceUpdatesRow
		if err := rows.Scan(
			&i.ChannelID,
			&i.Ts,
			&i.SemanticRank,
			&i.SemanticDistance,
			&i.LexicalRank,
			&i.RrfScore,
			&i.MessageText,
			&i.TextTokens,
			&i.QueryTokens,
			&i.LexicalScore,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const deleteOldMessages = `-- name: DeleteOldMessages :exec
DELETE
FROM messages_v3
WHERE channel_id = $1
  AND CAST(ts AS numeric) < EXTRACT(
        epoch
        FROM
        NOW() - $2 :: interval
                            )
`

type DeleteOldMessagesParams struct {
	ChannelID string
	OlderThan pgtype.Interval
}

func (q *Queries) DeleteOldMessages(ctx context.Context, arg DeleteOldMessagesParams) error {
	_, err := q.db.Exec(ctx, deleteOldMessages, arg.ChannelID, arg.OlderThan)
	return err
}

const getAlerts = `-- name: GetAlerts :many
WITH alert_counts AS (SELECT m.attrs -> 'incident_action' ->> 'service'  as service,
                             m.attrs -> 'incident_action' ->> 'alert'    as alert,
                             m.attrs -> 'incident_action' ->> 'priority' as priority,
                             COUNT(t.ts)                                 as thread_message_count
                      FROM messages_v3 m
                               LEFT JOIN messages_v3 t ON
                          t.channel_id = m.channel_id AND
                          t.parent_ts = m.ts
                      WHERE (
                          $1 :: text = '*'
                              OR m.attrs -> 'incident_action' ->> 'service' = $1 :: text
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
FROM alert_counts
`

type GetAlertsRow struct {
	Service            string
	Alert              string
	Priority           string
	ThreadMessageCount int64
}

func (q *Queries) GetAlerts(ctx context.Context, service string) ([]GetAlertsRow, error) {
	rows, err := q.db.Query(ctx, getAlerts, service)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetAlertsRow
	for rows.Next() {
		var i GetAlertsRow
		if err := rows.Scan(
			&i.Service,
			&i.Alert,
			&i.Priority,
			&i.ThreadMessageCount,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getAllMessages = `-- name: GetAllMessages :many
SELECT channel_id,
       ts,
       attrs
FROM messages_v3
WHERE channel_id = $1
  AND parent_ts IS NULL
ORDER BY (ts::float) DESC
LIMIT $2
`

type GetAllMessagesParams struct {
	ChannelID string
	N         int32
}

type GetAllMessagesRow struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) GetAllMessages(ctx context.Context, arg GetAllMessagesParams) ([]GetAllMessagesRow, error) {
	rows, err := q.db.Query(ctx, getAllMessages, arg.ChannelID, arg.N)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetAllMessagesRow
	for rows.Next() {
		var i GetAllMessagesRow
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getLatestServiceUpdates = `-- name: GetLatestServiceUpdates :many
WITH valid_messages AS (SELECT channel_id,
                               ts,
                               attrs,
                               embedding,
                               CASE
                                   WHEN attrs -> 'message' ->> 'text' = '' OR attrs -> 'message' ->> 'text' IS NULL
                                       THEN -1
                                   ELSE ts_rank(tsvec, plainto_tsquery('english', $1 :: text))
                                   END as lexical_score
                        FROM messages_v3
                        WHERE CAST(SPLIT_PART(ts, '.', 1) AS numeric) > EXTRACT(
                                epoch
                                FROM
                                NOW() - $2 :: interval
                                                                        )
                          AND attrs -> 'message' ->> 'user' != $3 :: text
                          AND attrs -> 'incident_action' ->> 'action' IS NULL),
     semantic_matches AS (SELECT channel_id,
                                 ts,
                                 ROW_NUMBER() OVER (
                                     ORDER BY
                                         embedding <=> $4
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
LIMIT 5
`

type GetLatestServiceUpdatesParams struct {
	QueryText      string
	Interval       pgtype.Interval
	BotID          string
	QueryEmbedding *pgvector.Vector
}

type GetLatestServiceUpdatesRow struct {
	ChannelID    string
	Ts           string
	Attrs        []byte
	SemanticRank int64
	LexicalRank  int64
	CRrfScore    float64
}

func (q *Queries) GetLatestServiceUpdates(ctx context.Context, arg GetLatestServiceUpdatesParams) ([]GetLatestServiceUpdatesRow, error) {
	rows, err := q.db.Query(ctx, getLatestServiceUpdates,
		arg.QueryText,
		arg.Interval,
		arg.BotID,
		arg.QueryEmbedding,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetLatestServiceUpdatesRow
	for rows.Next() {
		var i GetLatestServiceUpdatesRow
		if err := rows.Scan(
			&i.ChannelID,
			&i.Ts,
			&i.Attrs,
			&i.SemanticRank,
			&i.LexicalRank,
			&i.CRrfScore,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMessage = `-- name: GetMessage :one
SELECT channel_id,
       ts,
       attrs,
       embedding
FROM messages_v3
WHERE channel_id = $1
  AND ts = $2
`

type GetMessageParams struct {
	ChannelID string
	Ts        string
}

type GetMessageRow struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
	Embedding *pgvector.Vector
}

func (q *Queries) GetMessage(ctx context.Context, arg GetMessageParams) (GetMessageRow, error) {
	row := q.db.QueryRow(ctx, getMessage, arg.ChannelID, arg.Ts)
	var i GetMessageRow
	err := row.Scan(
		&i.ChannelID,
		&i.Ts,
		&i.Attrs,
		&i.Embedding,
	)
	return i, err
}

const getMessagesByUser = `-- name: GetMessagesByUser :many
SELECT channel_id,
       ts,
       attrs
FROM messages_v3
WHERE ts::float BETWEEN $1 AND $2
  AND attrs -> 'message' ->> 'user' = $3 :: text
`

type GetMessagesByUserParams struct {
	StartTs string
	EndTs   string
	UserID  string
}

type GetMessagesByUserRow struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) GetMessagesByUser(ctx context.Context, arg GetMessagesByUserParams) ([]GetMessagesByUserRow, error) {
	rows, err := q.db.Query(ctx, getMessagesByUser, arg.StartTs, arg.EndTs, arg.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetMessagesByUserRow
	for rows.Next() {
		var i GetMessagesByUserRow
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getMessagesWithinTS = `-- name: GetMessagesWithinTS :many
SELECT channel_id,
       ts,
       attrs
FROM messages_v3
WHERE channel_id = $1
  AND ts::float BETWEEN $2
    AND $3
`

type GetMessagesWithinTSParams struct {
	ChannelID string
	StartTs   string
	EndTs     string
}

type GetMessagesWithinTSRow struct {
	ChannelID string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) GetMessagesWithinTS(ctx context.Context, arg GetMessagesWithinTSParams) ([]GetMessagesWithinTSRow, error) {
	rows, err := q.db.Query(ctx, getMessagesWithinTS, arg.ChannelID, arg.StartTs, arg.EndTs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetMessagesWithinTSRow
	for rows.Next() {
		var i GetMessagesWithinTSRow
		if err := rows.Scan(&i.ChannelID, &i.Ts, &i.Attrs); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getServices = `-- name: GetServices :many
SELECT service :: text
FROM (SELECT DISTINCT attrs -> 'incident_action' ->> 'service' as service
      FROM messages_v3
      WHERE attrs -> 'incident_action' ->> 'service' IS NOT NULL
        AND parent_ts IS NULL) s
`

func (q *Queries) GetServices(ctx context.Context) ([]string, error) {
	rows, err := q.db.Query(ctx, getServices)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []string
	for rows.Next() {
		var service string
		if err := rows.Scan(&service); err != nil {
			return nil, err
		}
		items = append(items, service)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getThreadMessages = `-- name: GetThreadMessages :many
SELECT channel_id,
       parent_ts,
       ts,
       attrs
FROM (
    SELECT channel_id,
           parent_ts,
           ts,
           attrs
    FROM messages_v3
    WHERE channel_id = $1
      AND parent_ts = $2 :: text
      AND ($3 :: text = '' OR attrs -> 'message' ->> 'user' != $3 :: text)
    ORDER BY (ts::float) DESC
    LIMIT $4
) subquery
ORDER BY (ts::float) ASC
`

type GetThreadMessagesParams struct {
	ChannelID string
	ParentTs  string
	BotID     string
	LimitVal  int32
}

type GetThreadMessagesRow struct {
	ChannelID string
	ParentTs  *string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) GetThreadMessages(ctx context.Context, arg GetThreadMessagesParams) ([]GetThreadMessagesRow, error) {
	rows, err := q.db.Query(ctx, getThreadMessages,
		arg.ChannelID,
		arg.ParentTs,
		arg.BotID,
		arg.LimitVal,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetThreadMessagesRow
	for rows.Next() {
		var i GetThreadMessagesRow
		if err := rows.Scan(
			&i.ChannelID,
			&i.ParentTs,
			&i.Ts,
			&i.Attrs,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getThreadMessagesByServiceAndAlert = `-- name: GetThreadMessagesByServiceAndAlert :many
SELECT t.channel_id,
       t.parent_ts,
       t.ts,
       t.attrs
FROM messages_v3 t
         JOIN messages_v3 m ON m.channel_id = t.channel_id
    AND m.ts = t.parent_ts
WHERE m.attrs -> 'incident_action' ->> 'service' = $1 :: text
  AND m.attrs -> 'incident_action' ->> 'alert' = $2 :: text
  AND m.parent_ts IS NULL
  AND t.attrs -> 'message' ->> 'user' != $3 :: text
`

type GetThreadMessagesByServiceAndAlertParams struct {
	Service string
	Alert   string
	BotID   string
}

type GetThreadMessagesByServiceAndAlertRow struct {
	ChannelID string
	ParentTs  *string
	Ts        string
	Attrs     dto.MessageAttrs
}

func (q *Queries) GetThreadMessagesByServiceAndAlert(ctx context.Context, arg GetThreadMessagesByServiceAndAlertParams) ([]GetThreadMessagesByServiceAndAlertRow, error) {
	rows, err := q.db.Query(ctx, getThreadMessagesByServiceAndAlert, arg.Service, arg.Alert, arg.BotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetThreadMessagesByServiceAndAlertRow
	for rows.Next() {
		var i GetThreadMessagesByServiceAndAlertRow
		if err := rows.Scan(
			&i.ChannelID,
			&i.ParentTs,
			&i.Ts,
			&i.Attrs,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateMessageAttrs = `-- name: UpdateMessageAttrs :exec
UPDATE
    messages_v3
SET attrs     = COALESCE(attrs, '{}' :: jsonb) || $1,
    embedding = $2
WHERE channel_id = $3
  AND ts = $4
`

type UpdateMessageAttrsParams struct {
	Attrs     dto.MessageAttrs
	Embedding *pgvector.Vector
	ChannelID string
	Ts        string
}

func (q *Queries) UpdateMessageAttrs(ctx context.Context, arg UpdateMessageAttrsParams) error {
	_, err := q.db.Exec(ctx, updateMessageAttrs,
		arg.Attrs,
		arg.Embedding,
		arg.ChannelID,
		arg.Ts,
	)
	return err
}

const updateReaction = `-- name: UpdateReaction :exec
WITH reaction_count AS (SELECT COALESCE((attrs -> 'reactions' ->> ($1::text))::int, 0) + $4::int AS new_count
                        FROM messages_v3
                        WHERE channel_id = $2
                          AND ts = $3)
UPDATE messages_v3 m
SET attrs = jsonb_set(
        COALESCE(m.attrs, '{}'::jsonb),
        '{reactions}'::text[],
        CASE
            WHEN (SELECT new_count FROM reaction_count) <= 0 THEN
                COALESCE(m.attrs -> 'reactions', '{}'::jsonb) - $1::text
            ELSE
                jsonb_set(
                        COALESCE(m.attrs -> 'reactions', '{}'::jsonb),
                        array [$1::text],
                        to_jsonb((SELECT new_count FROM reaction_count))
                )
            END
            )
WHERE m.channel_id = $2
  AND m.ts = $3
`

type UpdateReactionParams struct {
	Reaction  string
	ChannelID string
	Ts        string
	Count     int32
}

func (q *Queries) UpdateReaction(ctx context.Context, arg UpdateReactionParams) error {
	_, err := q.db.Exec(ctx, updateReaction,
		arg.Reaction,
		arg.ChannelID,
		arg.Ts,
		arg.Count,
	)
	return err
}
