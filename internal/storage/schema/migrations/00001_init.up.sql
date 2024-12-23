CREATE TABLE channels
(
    channel_id         TEXT PRIMARY KEY,
    attrs              JSONB,
    created_at         TIMESTAMPTZ   DEFAULT now(),
    slack_ts_watermark TEXT NOT NULL DEFAULT (
        EXTRACT(EPOCH FROM now() - INTERVAL '14 days')::TEXT || '.000000'
        )
);

CREATE UNIQUE INDEX channels_name_idx ON channels (((attrs ->> 'name'))) WHERE attrs ->> 'name' IS NOT NULL;

CREATE TABLE messages
(
    channel_id TEXT REFERENCES channels (channel_id) ON DELETE CASCADE,
    slack_ts   TEXT NOT NULL,
    attrs      JSONB,

    PRIMARY KEY (channel_id, slack_ts)
);

CREATE TABLE incidents
(
    incident_id     SERIAL PRIMARY KEY,
    channel_id      TEXT        NOT NULL,
    slack_ts        TEXT        NOT NULL,
    alert           TEXT        NOT NULL,
    service         TEXT        NOT NULL,
    priority        TEXT        NOT NULL,
    attrs           JSONB,
    start_timestamp TIMESTAMPTZ NOT NULL,
    end_timestamp   TIMESTAMPTZ,

    FOREIGN KEY (channel_id, slack_ts) REFERENCES messages (channel_id, slack_ts) ON DELETE CASCADE,
    UNIQUE (channel_id, slack_ts)
);

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

-- Index for faster retrieval of reports by channel
CREATE INDEX reports_channel_id_created_at_idx ON reports (channel_id, created_at DESC);