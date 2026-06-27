-- name: InsertOnCallRotation :one
INSERT INTO oncall_rotations (
    name, service_id, pagerduty_schedule_id,
    manual_user_id, manual_display_name,
    override_start, override_end,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: GetOnCallRotation :one
SELECT id, name, service_id, pagerduty_schedule_id,
       manual_user_id, manual_display_name,
       override_start, override_end,
       created_at, updated_at
FROM oncall_rotations
WHERE id = $1;

-- name: UpdateOnCallRotation :exec
UPDATE oncall_rotations SET
    name                  = $2,
    service_id            = $3,
    pagerduty_schedule_id = $4,
    manual_user_id        = $5,
    manual_display_name   = $6,
    override_start        = $7,
    override_end          = $8,
    updated_at            = $9
WHERE id = $1;

-- name: DeleteOnCallRotation :exec
DELETE FROM oncall_rotations WHERE id = $1;

-- name: ListOnCallRotations :many
SELECT id, name, service_id, pagerduty_schedule_id,
       manual_user_id, manual_display_name,
       override_start, override_end,
       created_at, updated_at
FROM oncall_rotations
ORDER BY name;

-- name: GetCurrentOnCallForService :one
SELECT id, name, service_id, pagerduty_schedule_id,
       manual_user_id, manual_display_name,
       override_start, override_end,
       created_at, updated_at
FROM oncall_rotations
WHERE service_id = $1
  AND (
      -- prefer active manual override window
      (manual_user_id IS NOT NULL AND override_start <= NOW() AND override_end >= NOW())
      OR
      -- fall back to any rotation for this service
      (manual_user_id IS NULL OR override_start IS NULL OR override_end IS NULL)
  )
ORDER BY
    CASE WHEN manual_user_id IS NOT NULL AND override_start <= NOW() AND override_end >= NOW() THEN 0 ELSE 1 END,
    created_at DESC
LIMIT 1;
