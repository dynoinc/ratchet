-- name: AddLLMUsage :one
INSERT INTO
    llmusageV1 (input, output, model)
VALUES
    (@input, @output, @model)
RETURNING
    id, input, output, model, timestamp;

-- name: GetLLMUsageByTimeRange :many
SELECT
    id, input, output, model, timestamp
FROM
    llmusageV1
WHERE
    timestamp BETWEEN @start_time AND @end_time
ORDER BY
    timestamp DESC;

-- name: GetLLMUsageByModel :many
SELECT
    id, input, output, model, timestamp
FROM
    llmusageV1
WHERE
    model = @model
ORDER BY
    timestamp DESC
LIMIT @limit_val
OFFSET @offset_val;

-- name: PurgeLLMUsageOlderThan :execrows
DELETE FROM
    llmusageV1
WHERE
    timestamp < @older_than; 