ALTER TABLE channels
    ALTER COLUMN slack_ts_watermark
    SET DEFAULT (EXTRACT(EPOCH FROM now() - INTERVAL '14 days')::TEXT) || '.000000';