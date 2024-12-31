
BEGIN;

DROP INDEX reports_channel_id_created_at_idx;
DROP TABLE reports;

UPDATE channels
SET attrs = '{}'::jsonb
WHERE attrs IS NULL;
ALTER TABLE channels
    ALTER COLUMN attrs SET DEFAULT '{}'::jsonb,
    ALTER COLUMN attrs SET NOT NULL;

UPDATE messages
SET attrs = '{}'::jsonb
WHERE attrs IS NULL;
ALTER TABLE messages
    ALTER COLUMN attrs SET DEFAULT '{}'::jsonb,
    ALTER COLUMN attrs SET NOT NULL;

UPDATE incidents
SET attrs = '{}'::jsonb
WHERE attrs IS NULL;
ALTER TABLE incidents
    ALTER COLUMN attrs SET DEFAULT '{}'::jsonb,
    ALTER COLUMN attrs SET NOT NULL;

COMMIT;