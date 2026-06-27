-- M01: Full application schema

-- Sequence for generating SEV IDs (SEV-YYYY-NNNN)
CREATE SEQUENCE IF NOT EXISTS sev_number_seq START 1;

-- users (populated on first OAuth login in M03)
CREATE TABLE IF NOT EXISTS users (
    id              TEXT        PRIMARY KEY,
    email           TEXT        NOT NULL UNIQUE,
    name            TEXT        NOT NULL,
    avatar_url      TEXT,
    org_role        TEXT        NOT NULL DEFAULT 'viewer'
                        CHECK (org_role IN ('viewer', 'responder', 'incident-commander', 'admin')),
    active          BOOLEAN     NOT NULL DEFAULT TRUE,
    oauth_provider  TEXT        NOT NULL,
    oauth_subject   TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (oauth_provider, oauth_subject)
);

-- services (lightweight internal service registry)
CREATE TABLE IF NOT EXISTS services (
    id                    TEXT        PRIMARY KEY,
    name                  TEXT        NOT NULL UNIQUE,
    description           TEXT,
    owning_team           TEXT,
    pagerduty_service_id  TEXT,
    tags                  JSONB,
    active                BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- sevs (core incident record)
CREATE TABLE IF NOT EXISTS sevs (
    id                      TEXT        PRIMARY KEY,
    title                   TEXT        NOT NULL,
    description             TEXT        NOT NULL DEFAULT '',
    severity_level          SMALLINT    NOT NULL CHECK (severity_level BETWEEN 1 AND 4),
    status                  TEXT        NOT NULL DEFAULT 'open'
                                CHECK (status IN (
                                    'open', 'investigating', 'mitigated',
                                    'resolved', 'postmortem_in_progress', 'postmortem_complete'
                                )),
    root_cause_category     TEXT,
    root_cause_description  TEXT,
    mitigation              TEXT,
    prevention              TEXT,
    business_impact         TEXT,
    affected_services       TEXT[],
    detection_method        TEXT,
    alert_name              TEXT,
    monitoring_tool         TEXT,
    right_people_present    BOOLEAN,
    right_people_notes      TEXT,
    tags                    JSONB,
    started_at              TIMESTAMPTZ,
    detected_at             TIMESTAMPTZ,
    mitigated_at            TIMESTAMPTZ,
    resolved_at             TIMESTAMPTZ,
    postmortem_completed_at TIMESTAMPTZ,
    mttd_seconds            BIGINT,
    mttm_seconds            BIGINT,
    mttr_seconds            BIGINT,
    dttm_seconds            BIGINT,
    locked                  BOOLEAN     NOT NULL DEFAULT FALSE,
    sensitive               BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by              TEXT        NOT NULL,
    search_vector           TSVECTOR
);

CREATE INDEX IF NOT EXISTS idx_sevs_search        ON sevs USING GIN(search_vector);
CREATE INDEX IF NOT EXISTS idx_sevs_status        ON sevs(status);
CREATE INDEX IF NOT EXISTS idx_sevs_severity      ON sevs(severity_level);
CREATE INDEX IF NOT EXISTS idx_sevs_created_at    ON sevs(created_at DESC);

CREATE OR REPLACE FUNCTION update_sevs_search_vector() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('english',
        COALESCE(NEW.title,                  '') || ' ' ||
        COALESCE(NEW.description,            '') || ' ' ||
        COALESCE(NEW.root_cause_description, '') || ' ' ||
        COALESCE(NEW.business_impact,        '')
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tsvector_update_sevs
    BEFORE INSERT OR UPDATE ON sevs
    FOR EACH ROW EXECUTE FUNCTION update_sevs_search_vector();

-- sev_status_history (immutable transition log)
CREATE TABLE IF NOT EXISTS sev_status_history (
    id              BIGSERIAL   PRIMARY KEY,
    sev_id          TEXT        NOT NULL REFERENCES sevs(id),
    from_status     TEXT,
    to_status       TEXT        NOT NULL,
    user_id         TEXT        NOT NULL,
    transitioned_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sev_status_history_sev_id ON sev_status_history(sev_id);

-- sev_roles (people assigned to roles on a SEV)
CREATE TABLE IF NOT EXISTS sev_roles (
    id           BIGSERIAL   PRIMARY KEY,
    sev_id       TEXT        NOT NULL REFERENCES sevs(id),
    role_type    TEXT        NOT NULL
                     CHECK (role_type IN (
                         'on-call', 'detected-by', 'incident-commander',
                         'comms-lead', 'recorder', 'responder'
                     )),
    user_id      TEXT,
    display_name TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   TEXT        NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sev_roles_sev_id ON sev_roles(sev_id);

-- sev_announcements (ordered status updates with audience targeting)
CREATE TABLE IF NOT EXISTS sev_announcements (
    id            BIGSERIAL   PRIMARY KEY,
    sev_id        TEXT        NOT NULL REFERENCES sevs(id),
    author_id     TEXT        NOT NULL,
    message       TEXT        NOT NULL,
    audience      TEXT        NOT NULL
                      CHECK (audience IN ('internal', 'external', 'status-page')),
    is_milestone  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    search_vector TSVECTOR
);

CREATE INDEX IF NOT EXISTS idx_sev_announcements_sev_id ON sev_announcements(sev_id, created_at);
CREATE INDEX IF NOT EXISTS idx_sev_announcements_search ON sev_announcements USING GIN(search_vector);

CREATE OR REPLACE FUNCTION update_announcements_search_vector() RETURNS trigger AS $$
BEGIN
    NEW.search_vector := to_tsvector('english', COALESCE(NEW.message, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER tsvector_update_announcements
    BEFORE INSERT OR UPDATE ON sev_announcements
    FOR EACH ROW EXECUTE FUNCTION update_announcements_search_vector();

-- sev_chat_log (chat excerpts captured during an incident)
CREATE TABLE IF NOT EXISTS sev_chat_log (
    id          BIGSERIAL   PRIMARY KEY,
    sev_id      TEXT        NOT NULL REFERENCES sevs(id),
    occurred_at TIMESTAMPTZ NOT NULL,
    source      TEXT        NOT NULL,
    author      TEXT        NOT NULL,
    content     TEXT        NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    added_by    TEXT        NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sev_chat_log_sev_id ON sev_chat_log(sev_id, occurred_at);

-- sev_linked_tasks (external task references with SLA due dates)
CREATE TABLE IF NOT EXISTS sev_linked_tasks (
    id                BIGSERIAL   PRIMARY KEY,
    sev_id            TEXT        NOT NULL REFERENCES sevs(id),
    external_system   TEXT        NOT NULL,
    task_id           TEXT        NOT NULL,
    url               TEXT        NOT NULL,
    title             TEXT        NOT NULL,
    description       TEXT,
    relationship_type TEXT        NOT NULL
                          CHECK (relationship_type IN (
                              'action-item', 'contributing-factor', 'follow-up', 'dependency'
                          )),
    priority          TEXT        NOT NULL CHECK (priority IN ('critical', 'non-critical')),
    due_date          DATE,
    overdue           BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by        TEXT        NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sev_linked_tasks_sev_id ON sev_linked_tasks(sev_id);

-- sev_links (typed bidirectional SEV-to-SEV relationships)
-- Bidirectionality is enforced at the application layer: linking A→B inserts A→B and B→A.
CREATE TABLE IF NOT EXISTS sev_links (
    id                BIGSERIAL   PRIMARY KEY,
    source_sev_id     TEXT        NOT NULL REFERENCES sevs(id),
    target_sev_id     TEXT        NOT NULL REFERENCES sevs(id),
    relationship_type TEXT        NOT NULL
                          CHECK (relationship_type IN (
                              'related', 'caused-by', 'duplicate', 'recurrence-of'
                          )),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by        TEXT        NOT NULL,
    UNIQUE (source_sev_id, target_sev_id, relationship_type)
);

CREATE INDEX IF NOT EXISTS idx_sev_links_source ON sev_links(source_sev_id);
CREATE INDEX IF NOT EXISTS idx_sev_links_target ON sev_links(target_sev_id);

-- sev_slis (SLI violation records per SEV)
CREATE TABLE IF NOT EXISTS sev_slis (
    id              BIGSERIAL   PRIMARY KEY,
    sev_id          TEXT        NOT NULL REFERENCES sevs(id),
    service_id      TEXT        REFERENCES services(id),
    sli_name        TEXT        NOT NULL,
    slo_threshold   TEXT        NOT NULL,
    measured_value  TEXT        NOT NULL,
    violation_start TIMESTAMPTZ,
    violation_end   TIMESTAMPTZ,
    dashboard_url   TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sev_slis_sev_id ON sev_slis(sev_id);

-- postmortems (one per SEV, auto-created as Draft)
CREATE TABLE IF NOT EXISTS postmortems (
    id         BIGSERIAL   PRIMARY KEY,
    sev_id     TEXT        NOT NULL REFERENCES sevs(id) UNIQUE,
    status     TEXT        NOT NULL DEFAULT 'draft'
                   CHECK (status IN ('draft', 'in-review', 'approved')),
    content    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by TEXT
);

-- audit_log (immutable append-only; only audit_writer role has INSERT — no UPDATE or DELETE)
CREATE TABLE IF NOT EXISTS audit_log (
    id         BIGSERIAL   PRIMARY KEY,
    sev_id     TEXT        NOT NULL REFERENCES sevs(id),
    user_id    TEXT        NOT NULL,
    action     TEXT        NOT NULL,
    field_name TEXT,
    old_value  TEXT,
    new_value  TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_sev_id ON audit_log(sev_id, created_at);

-- ai_plugins (registered AI provider configurations)
CREATE TABLE IF NOT EXISTS ai_plugins (
    id                           BIGSERIAL   PRIMARY KEY,
    name                         TEXT        NOT NULL UNIQUE,
    version                      TEXT        NOT NULL,
    description                  TEXT,
    handler_type                 TEXT        NOT NULL CHECK (handler_type IN ('builtin', 'http')),
    http_endpoint                TEXT,
    provider                     TEXT,
    model                        TEXT,
    encrypted_api_key            BYTEA,
    enabled                      BOOLEAN     NOT NULL DEFAULT FALSE,
    trigger_on_open              BOOLEAN     NOT NULL DEFAULT TRUE,
    trigger_on_mitigated         BOOLEAN     NOT NULL DEFAULT TRUE,
    trigger_on_resolved          BOOLEAN     NOT NULL DEFAULT TRUE,
    trigger_on_postmortem_review BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ai_outputs (stored AI-generated content per SEV and trigger event)
CREATE TABLE IF NOT EXISTS ai_outputs (
    id            BIGSERIAL   PRIMARY KEY,
    sev_id        TEXT        NOT NULL REFERENCES sevs(id),
    plugin_id     BIGINT      NOT NULL REFERENCES ai_plugins(id),
    trigger_event TEXT        NOT NULL,
    action        TEXT        NOT NULL,
    content       TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_outputs_sev_id ON ai_outputs(sev_id);

-- oncall_rotations (on-call rotation definitions and manual overrides)
CREATE TABLE IF NOT EXISTS oncall_rotations (
    id                    BIGSERIAL   PRIMARY KEY,
    name                  TEXT        NOT NULL,
    service_id            TEXT        REFERENCES services(id),
    pagerduty_schedule_id TEXT,
    manual_user_id        TEXT,
    manual_display_name   TEXT,
    override_start        TIMESTAMPTZ,
    override_end          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oncall_rotations_service_id ON oncall_rotations(service_id);

-- integration_config (per-integration credentials and settings, credentials encrypted)
CREATE TABLE IF NOT EXISTS integration_config (
    id                    BIGSERIAL   PRIMARY KEY,
    integration_type      TEXT        NOT NULL UNIQUE,
    encrypted_credentials BYTEA,
    settings              JSONB,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- notification_config (role-based notification routing rules)
CREATE TABLE IF NOT EXISTS notification_config (
    id             BIGSERIAL   PRIMARY KEY,
    role           TEXT        NOT NULL,
    event          TEXT        NOT NULL,
    channel_type   TEXT        NOT NULL CHECK (channel_type IN ('slack', 'email')),
    channel_target TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (role, event, channel_type)
);

-- retention_config (per-severity-level retention policy; default is retain forever)
CREATE TABLE IF NOT EXISTS retention_config (
    id             BIGSERIAL   PRIMARY KEY,
    severity_level SMALLINT    NOT NULL UNIQUE CHECK (severity_level BETWEEN 1 AND 4),
    retention_days INT         NOT NULL DEFAULT 0,
    hard_delete    BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO retention_config (severity_level, retention_days, hard_delete) VALUES
    (1, 0, FALSE),
    (2, 0, FALSE),
    (3, 0, FALSE),
    (4, 0, FALSE)
ON CONFLICT (severity_level) DO NOTHING;

-- shareable_links (public read-only SEV link tokens)
CREATE TABLE IF NOT EXISTS shareable_links (
    id         BIGSERIAL   PRIMARY KEY,
    sev_id     TEXT        NOT NULL REFERENCES sevs(id),
    token      TEXT        NOT NULL UNIQUE,
    created_by TEXT        NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked    BOOLEAN     NOT NULL DEFAULT FALSE,
    revoked_by TEXT,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_shareable_links_token  ON shareable_links(token);
CREATE INDEX IF NOT EXISTS idx_shareable_links_sev_id ON shareable_links(sev_id);

-- audit_writer role: INSERT-only on audit_log
-- Defense-in-depth: the application layer also enforces append-only via the AuditStore interface.
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'audit_writer') THEN
        CREATE ROLE audit_writer;
    END IF;
END
$$;

GRANT INSERT ON audit_log TO audit_writer;
