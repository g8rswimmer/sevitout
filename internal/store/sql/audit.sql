-- name: AppendAuditEntry :one
INSERT INTO audit_log (sev_id, user_id, action, field_name, old_value, new_value, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: ListAuditEntriesBySEVID :many
SELECT id, sev_id, user_id, action, field_name, old_value, new_value, created_at
FROM audit_log
WHERE sev_id = $1
ORDER BY created_at;
