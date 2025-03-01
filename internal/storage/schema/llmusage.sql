-- name: AddLLMUsage :one
INSERT INTO
    llmusage (input, output, model, prompt)
VALUES
    (@input, @output, @model, @prompt)
RETURNING
    id, input, output, model, prompt, timestamp;

-- name: GetLLMUsageByID :one
SELECT
    id, input, output, model, prompt, timestamp
FROM
    llmusage
WHERE
    id = @id;

-- name: ListLLMUsage :many
SELECT
    id, input, output, model, prompt, timestamp
FROM
    llmusage
ORDER BY
    timestamp DESC
LIMIT @limit_val
OFFSET @offset_val;

-- name: GetLLMUsageByTimeRange :many
SELECT
    id, input, output, model, prompt, timestamp
FROM
    llmusage
WHERE
    timestamp BETWEEN @start_time AND @end_time
ORDER BY
    timestamp DESC;

-- name: GetLLMUsageByModel :many
SELECT
    id, input, output, model, prompt, timestamp
FROM
    llmusage
WHERE
    model = @model
ORDER BY
    timestamp DESC
LIMIT @limit_val
OFFSET @offset_val; 