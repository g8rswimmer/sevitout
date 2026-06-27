-- name: InsertService :exec
INSERT INTO services (id, name, description, owning_team, pagerduty_service_id, tags, active, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetService :one
SELECT id, name, description, owning_team, pagerduty_service_id, tags, active, created_at, updated_at
FROM services
WHERE id = $1;

-- name: UpdateService :exec
UPDATE services SET
    name                 = $2,
    description          = $3,
    owning_team          = $4,
    pagerduty_service_id = $5,
    tags                 = $6,
    active               = $7,
    updated_at           = $8
WHERE id = $1;

-- name: DeleteService :exec
DELETE FROM services WHERE id = $1;

-- name: ListServices :many
SELECT id, name, description, owning_team, pagerduty_service_id, tags, active, created_at, updated_at
FROM services
ORDER BY name;

-- name: ListActiveServices :many
SELECT id, name, description, owning_team, pagerduty_service_id, tags, active, created_at, updated_at
FROM services
WHERE active = true
ORDER BY name;
