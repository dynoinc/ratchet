CREATE TABLE IF NOT EXISTS channels (
    channel_id VARCHAR PRIMARY KEY,
    enabled BOOLEAN NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
     channel_id VARCHAR REFERENCES channels(channel_id) ON DELETE CASCADE,
     slack_ts VARCHAR NOT NULL,
     attrs JSONB,

     PRIMARY KEY (channel_id, slack_ts)
);