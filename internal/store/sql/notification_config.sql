-- name: UpsertNotificationConfig :one
INSERT INTO notification_config (role, event, channel_type, channel_target, max_severity_level, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
ON CONFLICT (role, event, channel_type) DO UPDATE SET
    channel_target     = EXCLUDED.channel_target,
    max_severity_level = EXCLUDED.max_severity_level,
    updated_at         = NOW()
RETURNING id, created_at, updated_at;

-- name: DeleteNotificationConfig :execrows
DELETE FROM notification_config
WHERE role = $1 AND event = $2 AND channel_type = $3;

-- name: ListNotificationConfigs :many
SELECT id, role, event, channel_type, channel_target, max_severity_level, created_at, updated_at
FROM notification_config
ORDER BY role, event, channel_type;

-- name: ListNotificationConfigsForEvent :many
SELECT id, role, event, channel_type, channel_target, max_severity_level, created_at, updated_at
FROM notification_config
WHERE event = $1
  AND (max_severity_level IS NULL OR sqlc.narg(severity_level)::smallint IS NULL OR max_severity_level >= sqlc.narg(severity_level))
ORDER BY role, channel_type;
