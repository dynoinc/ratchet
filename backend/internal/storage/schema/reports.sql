-- name: CreateReport :one
INSERT INTO reports (
    channel_id,
    report_period_start,
    report_period_end,
    report_data
) VALUES (
    $1, $2, $3, $4
) RETURNING *;

-- name: GetChannelReports :many
SELECT 
    r.id,
    r.channel_id,
    c.attrs->>'name' as channel_name,
    r.report_period_start,
    r.report_period_end,
    r.created_at
FROM reports r
JOIN channels c ON c.channel_id = r.channel_id
WHERE r.channel_id = $1
ORDER BY r.created_at DESC
LIMIT $2;

-- name: GetReport :one
SELECT 
    r.*,
    c.attrs->>'name' as channel_name
FROM reports r
JOIN channels c ON c.channel_id = r.channel_id
WHERE r.id = $1;