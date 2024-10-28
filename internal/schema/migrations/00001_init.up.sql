-- Enum types
CREATE TYPE activity_type_enum AS ENUM ('alert', 'human', 'bot');
CREATE TYPE user_type_enum AS ENUM ('human', 'bot');
CREATE TYPE severity_enum AS ENUM ('low', 'high');
CREATE TYPE root_cause_category_enum AS ENUM ('bug', 'dependency failure', 'misconfigured', 'other');

-- Table definitions
CREATE TABLE IF NOT EXISTS slack_channels (
    channel_id VARCHAR PRIMARY KEY,
    team_name VARCHAR NOT NULL,
    enabled BOOLEAN NOT NULL
);

CREATE TABLE IF NOT EXISTS slack_activities (
    channel_id VARCHAR NOT NULL,
    activity_slack_ts TIMESTAMPTZ NOT NULL,
    activity_type activity_type_enum NOT NULL,
    PRIMARY KEY (channel_id, activity_slack_ts),
    FOREIGN KEY (channel_id) REFERENCES slack_channels(channel_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS slack_messages (
    channel_id VARCHAR NOT NULL,
    activity_slack_ts TIMESTAMPTZ NOT NULL,
    message_channel_id VARCHAR NOT NULL,
    slack_ts TIMESTAMPTZ NOT NULL,
    user_id VARCHAR,
    user_type user_type_enum NOT NULL,
    text TEXT,
    reactions JSONB,
    PRIMARY KEY (channel_id, activity_slack_ts, message_channel_id, slack_ts),
    FOREIGN KEY (channel_id, activity_slack_ts) REFERENCES slack_activities(channel_id, activity_slack_ts) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS alerts (
    channel_id VARCHAR NOT NULL,
    activity_slack_ts TIMESTAMPTZ NOT NULL,
    triggered_ts TIMESTAMPTZ NOT NULL,
    resolved_ts TIMESTAMPTZ,
    alert_name VARCHAR NOT NULL,
    service VARCHAR NOT NULL,
    severity severity_enum NOT NULL,
    actionable BOOLEAN NOT NULL,
    root_cause_category root_cause_category_enum,
    root_cause TEXT,
    PRIMARY KEY (channel_id, activity_slack_ts),
    FOREIGN KEY (channel_id, activity_slack_ts) REFERENCES slack_activities(channel_id, activity_slack_ts) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS alerts_runbook (
    channel_id VARCHAR NOT NULL,
    alert_name VARCHAR NOT NULL,
    service VARCHAR NOT NULL,
    created_ts TIMESTAMPTZ NOT NULL,
    runbook TEXT NOT NULL,
    active BOOLEAN NOT NULL,
    source JSONB,
    PRIMARY KEY (channel_id, alert_name, service, created_ts),
    FOREIGN KEY (channel_id) REFERENCES slack_channels(channel_id) ON DELETE CASCADE
);

-- Indexes for optimizing specific queries
CREATE INDEX idx_alerts_unresolved ON alerts (channel_id, activity_slack_ts) WHERE resolved_ts IS NULL;
CREATE INDEX idx_alerts_runbook_active ON alerts_runbook (channel_id, alert_name, service) WHERE active;
