-- Phase 14: per-service, per-severity-level SEV leveling criteria — free-text
-- guidance for what qualifies as SEV-1..4 for a given service. Purely
-- advisory: unlike service_slas (Phase 12), nothing in internal/sev
-- evaluates this table; it's read only by the admin editor and the two
-- reference-display surfaces (SEV creation form, postmortem page). A row
-- only exists when there's guidance to show — "no criteria configured" is
-- "no row," not an empty string, so criteria is NOT NULL.
CREATE TABLE IF NOT EXISTS service_leveling_criteria (
    id             BIGSERIAL   PRIMARY KEY,
    service_id     TEXT        NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    severity_level SMALLINT    NOT NULL CHECK (severity_level BETWEEN 1 AND 4),
    criteria       TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (service_id, severity_level)
);

CREATE INDEX IF NOT EXISTS idx_service_leveling_criteria_service_id ON service_leveling_criteria(service_id);
