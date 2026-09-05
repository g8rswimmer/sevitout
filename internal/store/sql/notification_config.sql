-- name: CreateNotificationConfig :one
INSERT INTO notification_config (role, events, channel_type, channel_target, max_severity_level, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
RETURNING id, created_at, updated_at;

-- name: UpdateNotificationConfig :one
UPDATE notification_config SET
    role                = $2,
    events              = $3,
    channel_type        = $4,
    channel_target      = $5,
    max_severity_level  = $6,
    updated_at          = NOW()
WHERE id = $1
RETURNING id, created_at, updated_at;

-- name: DeleteNotificationConfig :execrows
DELETE FROM notification_config
WHERE id = $1;

-- name: ListNotificationConfigs :many
SELECT id, role, events, channel_type, channel_target, max_severity_level, created_at, updated_at
FROM notification_config
ORDER BY role, channel_type, id;

-- name: ListNotificationConfigsForEvent :many
SELECT id, role, events, channel_type, channel_target, max_severity_level, created_at, updated_at
FROM notification_config
WHERE sqlc.arg(event)::text = ANY(events)
  AND (max_severity_level IS NULL OR sqlc.narg(severity_level)::smallint IS NULL OR max_severity_level >= sqlc.narg(severity_level))
ORDER BY role, channel_type;
