-- name: CreateReport :one
INSERT INTO reports (
    channel_id,
    report_period_start,
    report_period_end,
    report_data
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetChannelReportsList :many
SELECT id, channel_id, report_period_start, report_period_end, created_at
FROM reports
WHERE channel_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: GetReport :one
SELECT *
FROM reports
WHERE id = $1;