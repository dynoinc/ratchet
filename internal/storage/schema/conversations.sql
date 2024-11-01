-- name: StartConversation :exec
INSERT INTO conversations (channel_id, slack_ts, attrs)
VALUES ($1, $2, $3)
ON CONFLICT (channel_id, slack_ts) DO NOTHING;

-- name: AddMessage :exec
INSERT INTO messages (channel_id, slack_ts, message_ts, created_at, attrs)
VALUES ($1, $2,$3, NOW(), $4);