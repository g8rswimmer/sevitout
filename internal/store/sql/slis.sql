-- name: InsertSLI :one
INSERT INTO sev_slis (
    sev_id, service_id, sli_name, slo_threshold, measured_value,
    violation_start, violation_end, dashboard_url, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: DeleteSLI :exec
DELETE FROM sev_slis WHERE id = $1;

-- name: ListSLIsBySEVID :many
SELECT id, sev_id, service_id, sli_name, slo_threshold, measured_value,
       violation_start, violation_end, dashboard_url, created_at
FROM sev_slis
WHERE sev_id = $1
ORDER BY created_at;
