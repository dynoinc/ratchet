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