-- name: StartConversation :exec
INSERT INTO conversations (channel_id, slack_ts, attrs)
VALUES ($1, $2, $3)
ON CONFLICT (channel_id, slack_ts) DO NOTHING;

-- name: AddMessage :exec
INSERT INTO messages (channel_id, slack_ts, message_ts, created_at, attrs)
VALUES ($1, $2,$3, NOW(), $4);

-- name: UpdateReactionCount :exec
WITH updated_data AS (
    SELECT
        channel_id,
        message_ts,
        CASE
            WHEN COALESCE((messages.attrs -> 'reactions' ->> @reaction::text)::int, 0) + @delta::int > 0
            THEN COALESCE(messages.attrs, '{}'::jsonb) || jsonb_build_object(
                    'reactions',
                    COALESCE(messages.attrs->'reactions', '{}'::jsonb) || jsonb_build_object(
                            @reaction::text, (COALESCE((messages.attrs -> 'reactions' ->> @reaction::text)::int, 0) + @delta::int)
                    )
            )
            ELSE COALESCE(messages.attrs, '{}'::jsonb) || jsonb_build_object(
                    'reactions', COALESCE(messages.attrs->'reactions', '{}'::jsonb) - @reaction::text
            )
            END AS new_data
    FROM messages
    WHERE messages.channel_id = @channel_id AND messages.message_ts = @message_ts
)
UPDATE messages
SET attrs = updated_data.new_data
FROM updated_data
WHERE messages.channel_id = updated_data.channel_id
  AND messages.message_ts = updated_data.message_ts;