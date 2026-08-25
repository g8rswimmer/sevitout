-- name: InsertSEVAccess :one
INSERT INTO sev_access (sev_id, user_id, created_at, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: DeleteSEVAccess :execresult
DELETE FROM sev_access WHERE id = $1 AND sev_id = $2;

-- name: ListSEVAccessBySEVID :many
SELECT id, sev_id, user_id, created_at, created_by
FROM sev_access
WHERE sev_id = $1
ORDER BY created_at;

-- name: SEVAccessExists :one
SELECT EXISTS(SELECT 1 FROM sev_access WHERE sev_id = $1 AND user_id = $2);

-- name: ListSEVIDsByAccessUser :many
SELECT sev_id FROM sev_access WHERE user_id = $1;
