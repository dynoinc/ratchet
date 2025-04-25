-- name: GetOrInsertDocumentationSource :one
INSERT INTO documentation_status (url)
VALUES (@url)
ON CONFLICT (url) DO UPDATE SET url = EXCLUDED.url
RETURNING *;

-- name: UpdateDocumentationSource :exec
UPDATE documentation_status
SET revision   = @revision,
    refresh_ts = now()
WHERE url = @url;

-- name: GetDocument :one
SELECT *
FROM documentation_docs
WHERE url = @url
  AND path = @path
  AND revision = @revision;

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
                 AND documentation_embeddings.revision != @revision
                 AND (SELECT needs_update FROM should_update)),
     doc_insert AS (
         INSERT INTO documentation_docs (url, path, revision, content)
             VALUES (@url, @path, @revision, @content)
             ON CONFLICT (url, path) DO UPDATE
                 SET content = EXCLUDED.content, revision = EXCLUDED.revision
                 WHERE (SELECT needs_update FROM should_update)
             RETURNING url, path, revision)
INSERT
INTO documentation_embeddings (url, path, revision, chunk_index, chunk, embedding)
SELECT (SELECT url FROM doc_insert),
       (SELECT path FROM doc_insert),
       (SELECT revision FROM doc_insert),
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
WITH closest_chunks AS (SELECT DISTINCT ON (e.url, e.path) e.url,
                                                           e.path,
                                                           e.revision,
                                                           e.chunk_index,
                                                           e.chunk,
                                                           e.embedding
                        FROM documentation_embeddings e
                        ORDER BY e.url, e.path, e.embedding <=> @embedding
                        LIMIT @limit_val)
SELECT c.url,
       c.path,
       c.revision,
       d.content
FROM closest_chunks c
         JOIN documentation_docs d ON c.url = d.url AND c.path = d.path AND c.revision = d.revision;

-- name: GetDocumentForUpdate :one
WITH closest_chunks AS (SELECT e.url,
                               e.path,
                               e.revision,
                               e.chunk_index,
                               e.chunk,
                               e.embedding
                        FROM documentation_embeddings e
                        ORDER BY e.embedding <=> @embedding
                        LIMIT 25),
     doc_counts AS (SELECT c.url,
                           c.path,
                           c.revision,
                           COUNT(*) as chunk_count
                    FROM closest_chunks c
                    GROUP BY c.url, c.path, c.revision
                    ORDER BY chunk_count DESC
                    LIMIT 1)
SELECT d.url,
       d.path,
       d.revision,
       d.content
FROM doc_counts dc
         JOIN documentation_docs d ON dc.url = d.url AND dc.path = d.path AND dc.revision = d.revision;

-- name: DebugGetDocumentForUpdate :many
WITH closest_chunks AS (
  SELECT e.url,
    e.path,
    e.revision,
    e.chunk_index,
    e.chunk,
    e.embedding,
    e.embedding <=> @embedding as distance
  FROM documentation_embeddings e
  ORDER BY e.embedding <=> @embedding
  LIMIT 25)
SELECT c.url,
      c.path,
      c.revision,
      c.chunk_index,
      c.chunk,
      c.distance,
      COUNT(*) OVER (PARTITION BY c.url, c.path, c.revision) as chunk_count,
      AVG(c.distance) OVER (PARTITION BY c.url, c.path, c.revision) as avg_distance,
      MIN(c.distance) OVER (PARTITION BY c.url, c.path, c.revision) as min_distance
FROM closest_chunks c
ORDER BY min_distance ASC, c.url, c.path, c.revision, c.chunk_index;

-- name: GetDocumentationStatus :many
SELECT 
    ds.url AS source,
    ds.revision,
    ds.refresh_ts,
    COUNT(DISTINCT dd.path) AS document_count,
    COUNT(de.chunk_index) AS chunk_count
FROM documentation_status ds
LEFT JOIN documentation_docs dd ON ds.url = dd.url
LEFT JOIN documentation_embeddings de ON ds.url = de.url
GROUP BY ds.url, ds.revision, ds.refresh_ts
ORDER BY ds.url;