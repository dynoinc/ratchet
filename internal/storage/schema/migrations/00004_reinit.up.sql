ALTER TABLE IF EXISTS thread_messages
    DROP CONSTRAINT IF EXISTS thread_messages_channel_id_fkey;
ALTER TABLE IF EXISTS thread_messages
    DROP CONSTRAINT IF EXISTS thread_messages_channel_id_slack_parent_ts_fkey;
DROP TABLE IF EXISTS thread_messages CASCADE;
DROP TABLE IF EXISTS incidents CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS channels CASCADE;


CREATE TABLE IF NOT EXISTS channels_v2
(
    id  TEXT PRIMARY KEY,
    attrs JSONB DEFAULT '{}'::JSONB
);


CREATE TABLE IF NOT EXISTS messages_v2
(
    channel_id TEXT NOT NULL REFERENCES channels_v2 (id) ON DELETE CASCADE,
    ts         TEXT NOT NULL,
    attrs      JSONB DEFAULT '{}'::JSONB,
    PRIMARY KEY (channel_id, ts)
);


CREATE TABLE IF NOT EXISTS thread_messages_v2
(
    channel_id TEXT NOT NULL REFERENCES channels_v2 (id) ON DELETE CASCADE,
    parent_ts  TEXT NOT NULL,
    ts         TEXT NOT NULL,
    attrs      JSONB DEFAULT '{}'::JSONB,
    PRIMARY KEY (channel_id, parent_ts, ts),
    CONSTRAINT fk_parent_message FOREIGN KEY (channel_id, parent_ts) REFERENCES messages_v2 (channel_id, ts) ON DELETE CASCADE
);