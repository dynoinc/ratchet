CREATE TABLE IF NOT EXISTS llm_usage_v1 (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW() NOT NULL,
    model TEXT NOT NULL,
    operation_type TEXT NOT NULL,
    prompt_text TEXT NOT NULL,
    completion_text TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    total_tokens INTEGER,
    latency_ms INTEGER,
    status TEXT NOT NULL,
    error_message TEXT,
    metadata JSONB DEFAULT '{}' :: JSONB
);

CREATE INDEX IF NOT EXISTS llm_usage_created_at_idx ON llm_usage_v1 (created_at);
CREATE INDEX IF NOT EXISTS llm_usage_model_idx ON llm_usage_v1 (model);
CREATE INDEX IF NOT EXISTS llm_usage_operation_idx ON llm_usage_v1 (operation_type);