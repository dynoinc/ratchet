-- name: RecordLLMUsage :one
INSERT INTO llm_usage_v1 (
    model,
    operation_type,
    prompt_text,
    completion_text,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    latency_ms,
    status,
    error_message,
    metadata
) VALUES (
    @model,
    @operation_type,
    @prompt_text,
    @completion_text,
    @prompt_tokens,
    @completion_tokens,
    @total_tokens,
    @latency_ms,
    @status,
    @error_message,
    @metadata
) RETURNING id, created_at, model, operation_type, status;

-- name: GetLLMUsageByTimeRange :many
SELECT 
    id,
    created_at,
    model,
    operation_type,
    prompt_text,
    completion_text,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    latency_ms,
    status,
    error_message,
    metadata
FROM llm_usage_v1
WHERE created_at BETWEEN @start_time AND @end_time
ORDER BY created_at DESC
LIMIT @results_limit OFFSET @results_offset;

-- name: GetLLMUsageStats :one
SELECT 
    COUNT(*) as total_requests,
    SUM(prompt_tokens) as total_prompt_tokens,
    SUM(completion_tokens) as total_completion_tokens,
    SUM(total_tokens) as total_tokens,
    AVG(latency_ms) as avg_latency_ms,
    COUNT(CASE WHEN status = 'error' THEN 1 END) as error_count
FROM llm_usage_v1
WHERE created_at BETWEEN @start_time AND @end_time
AND (@model = '' OR model = @model)
AND (@operation_type = '' OR operation_type = @operation_type);