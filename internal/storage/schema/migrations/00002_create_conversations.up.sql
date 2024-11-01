CREATE TABLE IF NOT EXISTS conversations (
    channel_id VARCHAR REFERENCES slack_channels(channel_id),
    slack_ts VARCHAR NOT NULL,
    attrs JSONB,

    PRIMARY KEY (channel_id, slack_ts)
);

CREATE TABLE IF NOT EXISTS messages (
    channel_id VARCHAR,
    slack_ts VARCHAR,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    attrs JSONB,

    PRIMARY KEY (channel_id, slack_ts)
);
