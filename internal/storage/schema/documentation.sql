-- name: GetOrInsertDocumentationSource :one
INSERT INTO documentation_status (url)
VALUES (@url)
ON CONFLICT (url) DO UPDATE SET url = EXCLUDED.url
RETURNING *;

-- name: UpdateDocumentationSource :exec
WITH delete_old_docs AS (
    DELETE FROM documentation_docs
    WHERE url = @url
      AND revision != @revision
)
UPDATE documentation_status
SET revision   = @revision, refresh_ts = now()
WHERE documentation_status.url = @url;

-- name: GetDocument :one
SELECT *
FROM documentation_docs
WHERE url = @url
  AND path = @path
  AND revision = @revision;

-- name: UpdateDocumentRevisionIfSHAMatches :one
UPDATE documentation_docs
SET revision = @new_revision
WHERE url = @url
  AND path = @path
  AND blob_sha = @blob_sha
RETURNING *;

-- name: InsertDocWithEmbeddings :exec
WITH should_update AS (SELECT (NOT EXISTS (SELECT 1
                                           FROM documentation_docs
                                           WHERE documentation_docs.url = @url
                                             AND documentation_docs.path = @path
                                             AND documentation_docs.revision = @revision)) AS needs_update),
     delete_old_embeddings AS (
         DELETE FROM documentation_embeddings
             WHERE documentation_embeddings.url = @url
                 AND documentation_embeddings.path = @path
                 AND documentation_embeddings.blob_sha != @blob_sha
                 AND (SELECT needs_update FROM should_update)),
     doc_insert AS (
         INSERT INTO documentation_docs (url, path, revision, blob_sha, content)
             VALUES (@url, @path, @revision, @blob_sha, @content)
             ON CONFLICT (url, path) DO UPDATE
                 SET content = EXCLUDED.content, revision = EXCLUDED.revision, blob_sha = EXCLUDED.blob_sha
                 WHERE (SELECT needs_update FROM should_update)
             RETURNING url, path, blob_sha)
INSERT
INTO documentation_embeddings (url, path, blob_sha, chunk_index, chunk, embedding)
SELECT (SELECT url FROM doc_insert),
       (SELECT path FROM doc_insert),
       (SELECT blob_sha FROM doc_insert),
       unnest(@chunk_indices::int[]),
       unnest(@chunks::text[]),
       unnest(@embeddings::vector(768)[])
WHERE (SELECT needs_update FROM should_update);

-- name: DeleteDoc :exec
DELETE
FROM documentation_docs
WHERE url = @url
  AND path = @path;

-- name: GetClosestDocs :many
WITH ranked_chunks AS (
    SELECT
        e.url,
        e.path,
        e.blob_sha,
        e.embedding <=> @embedding AS distance,
        ROW_NUMBER() OVER (PARTITION BY e.url, e.path, e.blob_sha ORDER BY e.embedding <=> @embedding ASC) as rn
    FROM
        documentation_embeddings e
),
closest_doc_chunks AS (
    SELECT
        url,
        path,
        blob_sha,
        distance
    FROM
        ranked_chunks
    WHERE
        rn = 1
)
SELECT
    cdc.url,
    cdc.path,
    d.revision,
    d.content
FROM
    closest_doc_chunks cdc
JOIN
    documentation_docs d ON cdc.url = d.url AND cdc.path = d.path AND cdc.blob_sha = d.blob_sha
ORDER BY
    cdc.distance ASC
LIMIT @limit_val;

-- name: DebugGetClosestDocs :many
WITH ranked_chunks AS (
    SELECT
        e.url,
        e.path,
        e.blob_sha,
        e.chunk_index,
        e.chunk,
        e.embedding <=> @embedding AS distance,
        ROW_NUMBER() OVER (PARTITION BY e.url, e.path, e.blob_sha ORDER BY e.embedding <=> @embedding ASC) as rn
    FROM
        documentation_embeddings e
),
closest_doc_chunks AS (
    SELECT
        url,
        path,
        blob_sha,
        chunk_index,
        chunk,
        distance
    FROM
        ranked_chunks
    WHERE
        rn = 1
)
SELECT
    *
FROM
    closest_doc_chunks
ORDER BY
    distance ASC
LIMIT @limit_val;

-- name: GetDocumentForUpdate :one
WITH closest_chunks AS (SELECT e.url,
                               e.path,
                               e.blob_sha,
                               e.chunk_index,
                               e.chunk,
                               e.embedding
                        FROM documentation_embeddings e
                        ORDER BY e.embedding <=> @embedding
                        LIMIT 25),
     doc_counts AS (SELECT c.url,
                           c.path,
                           c.blob_sha,
                           COUNT(*) as chunk_count
                    FROM closest_chunks c
                    GROUP BY c.url, c.path, c.blob_sha
                    ORDER BY chunk_count DESC
                    LIMIT 1)
SELECT d.url,
       d.path,
       d.revision,
       d.content,
       d.blob_sha
FROM doc_counts dc
         JOIN documentation_docs d ON dc.url = d.url AND dc.path = d.path AND dc.blob_sha = d.blob_sha;

-- name: DebugGetDocumentForUpdate :many
WITH closest_chunks AS (
  SELECT e.url,
    e.path,
    e.blob_sha,
    e.chunk_index,
    e.chunk,
    e.embedding,
    e.embedding <=> @embedding as distance
  FROM documentation_embeddings e
  ORDER BY e.embedding <=> @embedding
  LIMIT 25)
SELECT c.url,
      c.path,
      c.blob_sha,
      c.chunk_index,
      c.chunk,
      c.distance,
      COUNT(*) OVER (PARTITION BY c.url, c.path, c.blob_sha) as chunk_count,
      AVG(c.distance) OVER (PARTITION BY c.url, c.path, c.blob_sha) as avg_distance,
      MIN(c.distance) OVER (PARTITION BY c.url, c.path, c.blob_sha) as min_distance
FROM closest_chunks c
ORDER BY min_distance ASC, c.url, c.path, c.blob_sha, c.chunk_index;

-- name: GetDocumentationStatus :many
SELECT 
    ds.url AS source,
    ds.revision,
    ds.refresh_ts,
    (SELECT COUNT(path) FROM documentation_docs WHERE url = ds.url) AS document_count,
    (SELECT COUNT(chunk_index) FROM documentation_embeddings WHERE url = ds.url) AS chunk_count
FROM documentation_status ds
ORDER BY ds.url;

-- name: GetDocumentByPathSuffix :many
SELECT *
FROM documentation_docs
WHERE path LIKE '%' || @path_suffix;