-- name: OpenIncident :one
INSERT INTO incidents (channel_id,
                       slack_ts,
                       alert,
                       service,
                       priority,
                       attrs,
                       start_timestamp)
VALUES (@channel_id,
        @slack_ts,
        @alert,
        @service,
        @priority,
        @attrs,
        @start_timestamp)
ON CONFLICT (channel_id, slack_ts)
    DO UPDATE SET alert = EXCLUDED.alert
RETURNING incident_id;

-- name: GetLatestIncidentBeforeTimestamp :one
SELECT *
FROM incidents
WHERE channel_id = @channel_id
  AND alert = @alert
  AND service = @service
  AND start_timestamp < @before_timestamp
  AND end_timestamp IS NULL
ORDER BY start_timestamp DESC
LIMIT 1;

-- name: CloseIncident :one
UPDATE incidents
SET end_timestamp = @end_timestamp
WHERE incident_id = @incident_id::integer
RETURNING incident_id;

-- name: GetOpenIncidents :many
SELECT *
FROM incidents
WHERE end_timestamp IS NULL;

-- name: GetIncidentStatsByPeriod :many
SELECT priority                                                           as severity,
       COUNT(*)                                                           as count,
       AVG(EXTRACT(EPOCH FROM (end_timestamp - start_timestamp)))         as avg_duration_seconds,
       SUM(EXTRACT(EPOCH FROM (end_timestamp - start_timestamp)))::float8 as total_duration_seconds
FROM incidents
WHERE channel_id = $1
  AND start_timestamp >= $2
  AND start_timestamp <= $3
  AND end_timestamp IS NOT NULL
GROUP BY priority
ORDER BY priority;

-- name: GetTopAlerts :many
SELECT alert,
       COUNT(*)                                                   as count,
       MAX(start_timestamp)                                       as last_seen,
       AVG(EXTRACT(EPOCH FROM (end_timestamp - start_timestamp))) as avg_duration_seconds
FROM incidents
WHERE channel_id = $1
  AND start_timestamp >= $2
  AND start_timestamp <= $3
  AND end_timestamp IS NOT NULL
GROUP BY alert
ORDER BY count DESC
LIMIT 5;

-- name: GetAllIncidents :many
SELECT *
FROM incidents
WHERE channel_id = $1
ORDER BY start_timestamp DESC;