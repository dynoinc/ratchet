-- name: AddMessage :exec
INSERT INTO messages (channel_id, slack_ts, attrs)
VALUES (@channel_id, @slack_ts, @attrs)
ON CONFLICT (channel_id, slack_ts) DO NOTHING;

-- name: GetMessage :one
SELECT * FROM messages WHERE channel_id = @channel_id AND slack_ts = @slack_ts;

-- name: SetIncidentID :exec
UPDATE messages
SET attrs = jsonb_set(attrs, '{incident_id}', to_jsonb(@incident_id::integer))
WHERE channel_id = @channel_id AND slack_ts = @slack_ts;