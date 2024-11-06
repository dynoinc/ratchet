-- name: OpenIncident :one
INSERT INTO incidents (
    channel_id,
    slack_ts,
    alert,
    service,
    priority,
    attrs,
    start_timestamp
) VALUES (
    @channel_id,
    @slack_ts,
    @alert,
    @service,
    @priority,
    @attrs,
    @start_timestamp
)
ON CONFLICT (channel_id, slack_ts)
DO UPDATE SET
    alert = EXCLUDED.alert
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