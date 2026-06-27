-- name: InsertSEVLink :one
INSERT INTO sev_links (source_sev_id, target_sev_id, relationship_type, created_at, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: DeleteSEVLink :exec
DELETE FROM sev_links
WHERE source_sev_id = $1 AND target_sev_id = $2 AND relationship_type = $3;

-- name: ListSEVLinksBySEVID :many
SELECT id, source_sev_id, target_sev_id, relationship_type, created_at, created_by
FROM sev_links
WHERE source_sev_id = $1 OR target_sev_id = $1
ORDER BY created_at;
