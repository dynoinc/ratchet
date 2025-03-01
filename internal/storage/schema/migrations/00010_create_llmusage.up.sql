CREATE TABLE IF NOT EXISTS llmusage (
    id SERIAL PRIMARY KEY,
    input JSONB NOT NULL,
    output JSONB NOT NULL,
    model TEXT NOT NULL,
    prompt TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Create an index on timestamp for efficient querying by time
CREATE INDEX IF NOT EXISTS llmusage_timestamp_idx ON llmusage (timestamp);

-- Create an index on model for efficient filtering by model
CREATE INDEX IF NOT EXISTS llmusage_model_idx ON llmusage (model); 