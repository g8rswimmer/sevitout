-- name: GetEscalationConfig :one
SELECT id, severity_level, threshold_minutes, enabled, created_at, updated_at
FROM escalation_config
WHERE severity_level = $1;

-- name: UpsertEscalationConfig :one
INSERT INTO escalation_config (severity_level, threshold_minutes, enabled, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (severity_level) DO UPDATE SET
    threshold_minutes = EXCLUDED.threshold_minutes,
    enabled           = EXCLUDED.enabled,
    updated_at        = NOW()
RETURNING id, created_at, updated_at;

-- name: ListEscalationConfigs :many
SELECT id, severity_level, threshold_minutes, enabled, created_at, updated_at
FROM escalation_config
ORDER BY severity_level;
