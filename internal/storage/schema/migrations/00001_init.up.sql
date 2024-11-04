CREATE TABLE IF NOT EXISTS slack_channels (
    channel_id VARCHAR PRIMARY KEY,
    team_name VARCHAR NOT NULL,
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS conversations (
     channel_id VARCHAR REFERENCES slack_channels(channel_id) ON DELETE CASCADE,
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
    FOREIGN KEY (channel_id, slack_ts) REFERENCES conversations(channel_id, slack_ts) ON DELETE CASCADE,
    UNIQUE (channel_id, message_ts)
);

CREATE INDEX IF NOT EXISTS idx_channel_id_message_ts ON messages (channel_id, message_ts);