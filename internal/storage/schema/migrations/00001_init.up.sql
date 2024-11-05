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

CREATE UNIQUE INDEX unique_open_incident_per_alert_per_service ON incidents (alert, service) WHERE end_timestamp IS NULL;
