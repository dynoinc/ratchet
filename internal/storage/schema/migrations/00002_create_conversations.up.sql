CREATE TABLE IF NOT EXISTS conversations (
    channel_id VARCHAR REFERENCES slack_channels(channel_id),
    slack_ts VARCHAR NOT NULL,
    attrs JSONB,

    PRIMARY KEY (channel_id, slack_ts)
);

CREATE TABLE IF NOT EXISTS messages (
    channel_id VARCHAR NOT NULL,
    slack_ts VARCHAR NOT NULL,
    message_ts VARCHAR NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    attrs JSONB,

    PRIMARY KEY (channel_id, slack_ts, message_ts),
    FOREIGN KEY (channel_id, slack_ts) REFERENCES conversations(channel_id, slack_ts)
);
