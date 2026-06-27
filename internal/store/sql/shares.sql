-- name: InsertShareableLink :one
INSERT INTO shareable_links (sev_id, token, created_by, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: GetShareableLinkByToken :one
SELECT id, sev_id, token, created_by, expires_at, revoked, revoked_by, revoked_at, created_at
FROM shareable_links
WHERE token = $1;

-- name: RevokeShareableLink :exec
UPDATE shareable_links SET
    revoked    = true,
    revoked_by = $2,
    revoked_at = NOW()
WHERE token = $1;

-- name: ListShareableLinksBySEVID :many
SELECT id, sev_id, token, created_by, expires_at, revoked, revoked_by, revoked_at, created_at
FROM shareable_links
WHERE sev_id = $1
ORDER BY created_at DESC;
