ALTER TABLE thread_messages_v2
    DROP CONSTRAINT fk_parent_message;
DROP TABLE messages_v2;
DROP TABLE thread_messages_v2;
DELETE
FROM channels_v2;

CREATE TABLE IF NOT EXISTS messages_v3
(
    channel_id TEXT NOT NULL REFERENCES channels_v2 (id) ON DELETE CASCADE,
    ts         TEXT NOT NULL,
    parent_ts  TEXT,
    attrs      JSONB DEFAULT '{}' :: JSONB,
    embedding  vector(768),
    tsvec      tsvector GENERATED ALWAYS AS (to_tsvector('english', attrs -> 'message' ->> 'text')) STORED,

    PRIMARY KEY (channel_id, ts)
);

CREATE INDEX IF NOT EXISTS parent_ts_idx ON messages_v3 (channel_id, parent_ts) WHERE parent_ts IS NOT NULL;
CREATE INDEX IF NOT EXISTS embedding_idx ON messages_v3 USING hnsw (embedding vector_cosine_ops) WITH (ef_construction = 256);
CREATE INDEX IF NOT EXISTS tsvec_idx ON messages_v3 USING GIN (tsvec);