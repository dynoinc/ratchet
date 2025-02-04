CREATE TABLE IF NOT EXISTS incident_runbooks (
    id BIGSERIAL PRIMARY KEY,
    attrs JSONB DEFAULT '{}' :: JSONB
);