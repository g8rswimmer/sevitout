-- Phase 12: per-service, per-severity-level SLA targets for MTTD/MTTM/MTTR.
-- A NULL target column means "no SLA set for that metric" — not an instant
-- breach (see internal/sev/sla.go's EvaluateSLA).
CREATE TABLE IF NOT EXISTS service_slas (
    id                   BIGSERIAL   PRIMARY KEY,
    service_id           TEXT        NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    severity_level       SMALLINT    NOT NULL CHECK (severity_level BETWEEN 1 AND 4),
    mttd_target_seconds  BIGINT,
    mttm_target_seconds  BIGINT,
    mttr_target_seconds  BIGINT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (service_id, severity_level)
);

CREATE INDEX IF NOT EXISTS idx_service_slas_service_id ON service_slas(service_id);
