-- name: OpenIncident :one
INSERT INTO incidents (
    channel_id,
    slack_ts,
    alert,
    service,
    priority,
    start_timestamp
) VALUES (
    @channel_id,
    @slack_ts,
    @alert,
    @service,
    @priority,
    now()
)
ON CONFLICT (channel_id, slack_ts)
DO UPDATE SET
    alert = EXCLUDED.alert
RETURNING incident_id;

-- name: FindActiveIncident :one
SELECT incident_id
FROM incidents
WHERE alert = @alert
  AND service = @service
  AND end_timestamp IS NULL
LIMIT 1;

-- name: CloseIncident :one
UPDATE incidents
SET end_timestamp = now()
WHERE incident_id = @incident_id::integer
RETURNING incident_id;