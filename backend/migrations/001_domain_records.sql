CREATE TABLE IF NOT EXISTS framemio_domain_records (
    kind TEXT NOT NULL,
    id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    project_id TEXT NOT NULL DEFAULT '',
    media_id TEXT NOT NULL DEFAULT '',
    logical_key TEXT NOT NULL DEFAULT '',
    data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (kind, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS framemio_domain_records_logical_key
    ON framemio_domain_records (kind, user_id, logical_key)
    WHERE logical_key <> '';

CREATE INDEX IF NOT EXISTS framemio_domain_records_project
    ON framemio_domain_records (kind, user_id, project_id, created_at);
