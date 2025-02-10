CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE
    messages_v2
ADD
    COLUMN embedding vector(768);

CREATE INDEX ON messages_v2 USING GIN (
    to_tsvector('english', attrs -> 'message' ->> 'text')
);

CREATE INDEX ON messages_v2 USING hnsw (embedding vector_cosine_ops) WITH (ef_construction = 256);