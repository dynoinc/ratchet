// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.27.0
// source: reports.sql

package schema

import (
	"context"

	dto "github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
)

const createReport = `-- name: CreateReport :one
INSERT INTO reports (channel_id,
                     report_period_start,
                     report_period_end,
                     report_data)
VALUES ($1, $2, $3, $4)
RETURNING id, channel_id, report_period_start, report_period_end, report_data, created_at
`

type CreateReportParams struct {
	ChannelID         string
	ReportPeriodStart pgtype.Timestamptz
	ReportPeriodEnd   pgtype.Timestamptz
	ReportData        dto.ReportData
}

func (q *Queries) CreateReport(ctx context.Context, arg CreateReportParams) (Report, error) {
	row := q.db.QueryRow(ctx, createReport,
		arg.ChannelID,
		arg.ReportPeriodStart,
		arg.ReportPeriodEnd,
		arg.ReportData,
	)
	var i Report
	err := row.Scan(
		&i.ID,
		&i.ChannelID,
		&i.ReportPeriodStart,
		&i.ReportPeriodEnd,
		&i.ReportData,
		&i.CreatedAt,
	)
	return i, err
}

const getChannelReports = `-- name: GetChannelReports :many
SELECT r.id,
       r.channel_id,
       c.attrs ->> 'name' as channel_name,
       r.report_period_start,
       r.report_period_end,
       r.created_at
FROM reports r
         JOIN channels c ON c.channel_id = r.channel_id
WHERE r.channel_id = $1
ORDER BY r.created_at DESC
LIMIT $2
`

type GetChannelReportsParams struct {
	ChannelID string
	Limit     int32
}

type GetChannelReportsRow struct {
	ID                int32
	ChannelID         string
	ChannelName       interface{}
	ReportPeriodStart pgtype.Timestamptz
	ReportPeriodEnd   pgtype.Timestamptz
	CreatedAt         pgtype.Timestamptz
}

func (q *Queries) GetChannelReports(ctx context.Context, arg GetChannelReportsParams) ([]GetChannelReportsRow, error) {
	rows, err := q.db.Query(ctx, getChannelReports, arg.ChannelID, arg.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetChannelReportsRow
	for rows.Next() {
		var i GetChannelReportsRow
		if err := rows.Scan(
			&i.ID,
			&i.ChannelID,
			&i.ChannelName,
			&i.ReportPeriodStart,
			&i.ReportPeriodEnd,
			&i.CreatedAt,
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

const getReport = `-- name: GetReport :one
SELECT r.id, r.channel_id, r.report_period_start, r.report_period_end, r.report_data, r.created_at,
       c.attrs ->> 'name' as channel_name
FROM reports r
         JOIN channels c ON c.channel_id = r.channel_id
WHERE r.id = $1
`

type GetReportRow struct {
	ID                int32
	ChannelID         string
	ReportPeriodStart pgtype.Timestamptz
	ReportPeriodEnd   pgtype.Timestamptz
	ReportData        dto.ReportData
	CreatedAt         pgtype.Timestamptz
	ChannelName       interface{}
}

func (q *Queries) GetReport(ctx context.Context, id int32) (GetReportRow, error) {
	row := q.db.QueryRow(ctx, getReport, id)
	var i GetReportRow
	err := row.Scan(
		&i.ID,
		&i.ChannelID,
		&i.ReportPeriodStart,
		&i.ReportPeriodEnd,
		&i.ReportData,
		&i.CreatedAt,
		&i.ChannelName,
	)
	return i, err
}
