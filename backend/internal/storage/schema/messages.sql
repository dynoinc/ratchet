-- name: AddMessage :exec
INSERT INTO messages (channel_id, slack_ts, attrs)
VALUES (@channel_id, @slack_ts, @attrs)
ON CONFLICT (channel_id, slack_ts) DO NOTHING;

-- name: GetMessage :one
SELECT * FROM messages WHERE channel_id = @channel_id AND slack_ts = @slack_ts;

-- name: SetIncidentID :exec
UPDATE messages
SET attrs = attrs || jsonb_build_object('incident_id', @incident_id::integer, 'action', @action::text)
WHERE channel_id = @channel_id AND slack_ts = @slack_ts;

-- name: TagAsBotNotification :exec
UPDATE messages
SET attrs = attrs || jsonb_build_object('bot_name', @bot_name::text)
WHERE channel_id = @channel_id AND slack_ts = @slack_ts;

-- name: TagAsUserMessage :exec
UPDATE messages
SET attrs = attrs || jsonb_build_object('user_id', @user_id::text)
WHERE channel_id = @channel_id AND slack_ts = @slack_ts;
