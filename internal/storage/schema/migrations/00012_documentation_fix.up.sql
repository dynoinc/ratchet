BEGIN;

DELETE FROM documentation_docs;
DELETE FROM documentation_embeddings;

ALTER TABLE documentation_docs
    ADD COLUMN blob_sha TEXT NOT NULL DEFAULT '';

ALTER TABLE documentation_docs
    ADD CONSTRAINT documentation_docs_url_path_blob_sha_key UNIQUE (url, path, blob_sha);

CREATE INDEX documentation_docs_url_path_blob_sha_idx ON documentation_docs (url, path, blob_sha);

ALTER TABLE documentation_embeddings
    RENAME COLUMN revision TO blob_sha;

ALTER TABLE documentation_embeddings
    DROP CONSTRAINT documentation_embeddings_url_path_revision_fkey;

ALTER TABLE documentation_embeddings
    ADD CONSTRAINT documentation_embeddings_url_path_blob_sha_fkey
    FOREIGN KEY (url, path, blob_sha) REFERENCES documentation_docs (url, path, blob_sha) ON DELETE CASCADE;

COMMIT;