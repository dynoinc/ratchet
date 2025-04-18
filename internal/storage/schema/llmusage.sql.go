// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.29.0
// source: llmusage.sql

package schema

import (
	"context"

	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
)

const addLLMUsage = `-- name: AddLLMUsage :one
INSERT INTO llmusageV1 (input, output, model)
VALUES ($1, $2, $3)
RETURNING
    id, input, output, model, timestamp
`

type AddLLMUsageParams struct {
	Input  dto.LLMInput
	Output dto.LLMOutput
	Model  string
}

func (q *Queries) AddLLMUsage(ctx context.Context, arg AddLLMUsageParams) (Llmusagev1, error) {
	row := q.db.QueryRow(ctx, addLLMUsage, arg.Input, arg.Output, arg.Model)
	var i Llmusagev1
	err := row.Scan(
		&i.ID,
		&i.Input,
		&i.Output,
		&i.Model,
		&i.Timestamp,
	)
	return i, err
}

const getLLMUsageByModel = `-- name: GetLLMUsageByModel :many
SELECT id,
       input,
       output,
       model,
       timestamp
FROM llmusageV1
WHERE model = $1
ORDER BY timestamp DESC
LIMIT $3 OFFSET $2
`

type GetLLMUsageByModelParams struct {
	Model     string
	OffsetVal int32
	LimitVal  int32
}

func (q *Queries) GetLLMUsageByModel(ctx context.Context, arg GetLLMUsageByModelParams) ([]Llmusagev1, error) {
	rows, err := q.db.Query(ctx, getLLMUsageByModel, arg.Model, arg.OffsetVal, arg.LimitVal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Llmusagev1
	for rows.Next() {
		var i Llmusagev1
		if err := rows.Scan(
			&i.ID,
			&i.Input,
			&i.Output,
			&i.Model,
			&i.Timestamp,
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

const getLLMUsageByTimeRange = `-- name: GetLLMUsageByTimeRange :many
SELECT id,
       input,
       output,
       model,
       timestamp
FROM llmusageV1
WHERE timestamp BETWEEN $1 AND $2
ORDER BY timestamp DESC
`

type GetLLMUsageByTimeRangeParams struct {
	StartTime pgtype.Timestamptz
	EndTime   pgtype.Timestamptz
}

func (q *Queries) GetLLMUsageByTimeRange(ctx context.Context, arg GetLLMUsageByTimeRangeParams) ([]Llmusagev1, error) {
	rows, err := q.db.Query(ctx, getLLMUsageByTimeRange, arg.StartTime, arg.EndTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Llmusagev1
	for rows.Next() {
		var i Llmusagev1
		if err := rows.Scan(
			&i.ID,
			&i.Input,
			&i.Output,
			&i.Model,
			&i.Timestamp,
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

const purgeLLMUsageOlderThan = `-- name: PurgeLLMUsageOlderThan :execrows
DELETE
FROM llmusageV1
WHERE timestamp < $1
`

func (q *Queries) PurgeLLMUsageOlderThan(ctx context.Context, olderThan pgtype.Timestamptz) (int64, error) {
	result, err := q.db.Exec(ctx, purgeLLMUsageOlderThan, olderThan)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
