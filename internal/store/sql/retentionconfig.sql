-- name: GetRetentionConfig :one
SELECT id, severity_level, retention_days, hard_delete, created_at, updated_at
FROM retention_config
WHERE severity_level = $1;

-- name: UpsertRetentionConfig :one
INSERT INTO retention_config (severity_level, retention_days, hard_delete, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (severity_level) DO UPDATE SET
    retention_days = EXCLUDED.retention_days,
    hard_delete    = EXCLUDED.hard_delete,
    updated_at     = NOW()
RETURNING id;

-- name: ListRetentionConfig :many
SELECT id, severity_level, retention_days, hard_delete, created_at, updated_at
FROM retention_config
ORDER BY severity_level;
