-- name: InsertSlackChannel :one
INSERT INTO slack_channels (
    channel_id,
    team_name,
    enabled
) VALUES (
    $1,
    $2,
    $3
)
RETURNING *;

-- name: UpdateSlackChannel :one
UPDATE slack_channels
SET team_name = $2,
    enabled = TRUE
WHERE channel_id = $1
RETURNING *;

-- name: DisableSlackChannel :one
UPDATE slack_channels
SET enabled = FALSE
WHERE channel_id = $1
RETURNING *;

-- name: GetSlackChannelByID :one
SELECT * FROM slack_channels
WHERE channel_id = $1;

-- name: GetSlackChannelsByTeamName :many
SELECT * FROM slack_channels
WHERE team_name = $1;

-- name: GetUniqueTeamNames :many
SELECT DISTINCT team_name FROM slack_channels;
