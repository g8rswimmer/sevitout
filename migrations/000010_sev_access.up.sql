-- sev_access: explicit per-user visibility grants for SEVs flagged
-- Sensitive (docs/requirements.md §14 — "only explicitly added users can
-- view"). Only Sensitive SEVs ever consult this table; a non-sensitive SEV
-- is visible to every authenticated user as before.
CREATE TABLE IF NOT EXISTS sev_access (
    id         BIGSERIAL   PRIMARY KEY,
    sev_id     TEXT        NOT NULL REFERENCES sevs(id),
    user_id    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT        NOT NULL,
    UNIQUE (sev_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_sev_access_sev_id  ON sev_access(sev_id);
CREATE INDEX IF NOT EXISTS idx_sev_access_user_id ON sev_access(user_id);
