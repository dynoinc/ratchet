CREATE TABLE reports
(
    id                  SERIAL PRIMARY KEY,
    channel_id          VARCHAR     NOT NULL REFERENCES channels (channel_id) ON DELETE CASCADE,
    report_period_start TIMESTAMPTZ NOT NULL,
    report_period_end   TIMESTAMPTZ NOT NULL,
    report_data         JSONB       NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Ensure we only have one report per channel per time period
    UNIQUE (channel_id, report_period_start, report_period_end)
);
CREATE INDEX reports_channel_id_created_at_idx ON reports (channel_id, created_at DESC);

