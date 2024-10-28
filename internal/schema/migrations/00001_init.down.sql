-- Drop indexes
DROP INDEX IF EXISTS idx_alerts_unresolved;
DROP INDEX IF EXISTS idx_alerts_runbook_active;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS alerts_runbook;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS slack_messages;
DROP TABLE IF EXISTS slack_activities;
DROP TABLE IF EXISTS slack_channels;

-- Drop enum types
DROP TYPE IF EXISTS activity_type_enum;
DROP TYPE IF EXISTS user_type_enum;
DROP TYPE IF EXISTS severity_enum;
DROP TYPE IF EXISTS root_cause_category_enum;
