-- name: InsertPostmortem :one
INSERT INTO postmortems (sev_id, status, content, created_at, updated_at, updated_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: GetPostmortemBySEVID :one
SELECT id, sev_id, status, content, created_at, updated_at, updated_by
FROM postmortems
WHERE sev_id = $1;

-- name: UpdatePostmortem :exec
UPDATE postmortems SET
    status     = $2,
    content    = $3,
    updated_at = $4,
    updated_by = $5
WHERE sev_id = $1;

-- name: CountPostmortemsByStatus :many
SELECT status, COUNT(*) AS count
FROM postmortems
GROUP BY status;
