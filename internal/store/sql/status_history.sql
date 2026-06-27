-- name: InsertStatusHistory :one
INSERT INTO sev_status_history (sev_id, from_status, to_status, user_id, transitioned_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: ListStatusHistoryBySEVID :many
SELECT id, sev_id, from_status, to_status, user_id, transitioned_at
FROM sev_status_history
WHERE sev_id = $1
ORDER BY transitioned_at;
