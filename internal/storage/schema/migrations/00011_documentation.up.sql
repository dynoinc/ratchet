CREATE TABLE IF NOT EXISTS documentation_status
(
    url        TEXT PRIMARY KEY,
    revision   TEXT        NOT NULL DEFAULT '',
    refresh_ts TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01T00:00:00Z'
);

CREATE TABLE IF NOT EXISTS documentation_docs
(
    url      TEXT NOT NULL,
    path     TEXT NOT NULL,

    revision TEXT NOT NULL,
    content  TEXT NOT NULL,

    PRIMARY KEY (url, path),
    UNIQUE (url, path, revision),
    FOREIGN KEY (url) REFERENCES documentation_status (url) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS documentation_embeddings
(
    url         TEXT        NOT NULL,
    path        TEXT        NOT NULL,
    revision    TEXT        NOT NULL,

    chunk_index INT         NOT NULL,
    chunk       TEXT        NOT NULL,
    embedding   vector(768) NOT NULL,

    PRIMARY KEY (url, path, revision, chunk_index),
    FOREIGN KEY (url, path, revision) REFERENCES documentation_docs (url, path, revision) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS documentation_embeddings_embedding_idx ON documentation_embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (ef_construction = 256);