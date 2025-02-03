-- name: AddChannel :one
INSERT INTO channels_v2 (id)
VALUES (@id)
ON CONFLICT (id) DO UPDATE
SET id = EXCLUDED.id
RETURNING id, attrs;

-- name: UpdateChannelAttrs :exec
UPDATE channels_v2
SET attrs = COALESCE(attrs, '{}'::jsonb) || @attrs
WHERE id = @id;

-- name: GetAllChannels :many
SELECT id, attrs FROM channels_v2;

-- name: GetChannelByName :one
SELECT id, attrs FROM channels_v2
WHERE attrs->>'name' = @name::text;
