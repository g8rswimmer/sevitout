-- name: InsertSEVRole :one
INSERT INTO sev_roles (sev_id, role_type, user_id, display_name, created_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: DeleteSEVRole :exec
DELETE FROM sev_roles WHERE id = $1;

-- name: ListRolesBySEVID :many
SELECT id, sev_id, role_type, user_id, display_name, created_at, created_by
FROM sev_roles
WHERE sev_id = $1
ORDER BY created_at;
