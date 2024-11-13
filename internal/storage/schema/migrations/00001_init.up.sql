CREATE TABLE channels (
    channel_id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ DEFAULT now(),
    latest_slack_ts TEXT NOT NULL DEFAULT (
        EXTRACT(EPOCH FROM now() - INTERVAL '14 days')::TEXT || '.000000'
    )
);

CREATE TABLE messages (
    channel_id TEXT REFERENCES channels(channel_id) ON DELETE CASCADE,
    slack_ts TEXT NOT NULL,
    attrs JSONB,

    PRIMARY KEY (channel_id, slack_ts)
);

CREATE TABLE incidents (
    incident_id SERIAL PRIMARY KEY,
    channel_id TEXT NOT NULL,
    slack_ts TEXT NOT NULL,
    alert TEXT NOT NULL,
    service TEXT NOT NULL,
    priority TEXT NOT NULL,
    attrs JSONB,
    start_timestamp TIMESTAMPTZ NOT NULL,
    end_timestamp TIMESTAMPTZ,

    FOREIGN KEY (channel_id, slack_ts) REFERENCES messages(channel_id, slack_ts) ON DELETE CASCADE,
    UNIQUE (channel_id, slack_ts)
);