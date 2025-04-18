CREATE TABLE IF NOT EXISTS llmusageV1
(
    id        SERIAL PRIMARY KEY,
    input     JSONB       NOT NULL,
    output    JSONB       NOT NULL,
    model     TEXT        NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create an index on timestamp for efficient querying by time
CREATE INDEX IF NOT EXISTS llmusageV1_timestamp_idx ON llmusageV1 (timestamp);

-- Create an index on model for efficient filtering by model
CREATE INDEX IF NOT EXISTS llmusageV1_model_idx ON llmusageV1 (model); 