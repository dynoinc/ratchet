CREATE TABLE channels (
    channel_id VARCHAR PRIMARY KEY,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE messages (
    channel_id VARCHAR REFERENCES channels(channel_id) ON DELETE CASCADE,
    slack_ts VARCHAR NOT NULL,
    attrs JSONB,

    PRIMARY KEY (channel_id, slack_ts)
);

CREATE TABLE incidents (
    incident_id SERIAL PRIMARY KEY,
    channel_id VARCHAR NOT NULL,
    slack_ts VARCHAR NOT NULL,
    alert VARCHAR NOT NULL,
    service VARCHAR NOT NULL,
    priority VARCHAR NOT NULL,
    attrs JSONB,
    start_timestamp TIMESTAMPTZ NOT NULL,
    end_timestamp TIMESTAMPTZ,

    FOREIGN KEY (channel_id, slack_ts) REFERENCES messages(channel_id, slack_ts) ON DELETE CASCADE,
    UNIQUE (channel_id, slack_ts)
);

CREATE TABLE reports (
    id SERIAL PRIMARY KEY,
    channel_id VARCHAR NOT NULL REFERENCES channels(channel_id) ON DELETE CASCADE,
    report_period_start TIMESTAMPTZ NOT NULL,
    report_period_end TIMESTAMPTZ NOT NULL,
    report_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Ensure we only have one report per channel per time period
    UNIQUE (channel_id, report_period_start, report_period_end)
);

-- Index for faster retrieval of reports by channel
CREATE INDEX reports_channel_id_created_at_idx ON reports(channel_id, created_at DESC); 