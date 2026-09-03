ALTER TABLE notification_config ADD COLUMN max_severity_level SMALLINT;

CREATE TABLE IF NOT EXISTS escalation_config (
    id                BIGSERIAL   PRIMARY KEY,
    severity_level    SMALLINT    NOT NULL CHECK (severity_level BETWEEN 1 AND 4) UNIQUE,
    threshold_minutes INT         NOT NULL DEFAULT 0,
    enabled           BOOLEAN     NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Pre-seeded disabled for all four severity levels, matching
-- retention_config's seed-data precedent above, so Postgres behaves the same
-- as the in-memory dev fallback (memory.NewEscalationConfigStore) from a
-- fresh start.
INSERT INTO escalation_config (severity_level, threshold_minutes, enabled) VALUES
    (1, 0, FALSE),
    (2, 0, FALSE),
    (3, 0, FALSE),
    (4, 0, FALSE)
ON CONFLICT (severity_level) DO NOTHING;

ALTER TABLE sevs ADD COLUMN escalated_at TIMESTAMPTZ;
