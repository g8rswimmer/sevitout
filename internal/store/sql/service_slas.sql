-- name: GetServiceSLA :one
SELECT id, service_id, severity_level, mttd_target_seconds, mttm_target_seconds, mttr_target_seconds, created_at, updated_at, mttpc_target_seconds
FROM service_slas
WHERE service_id = $1 AND severity_level = $2;

-- name: UpsertServiceSLA :one
INSERT INTO service_slas (service_id, severity_level, mttd_target_seconds, mttm_target_seconds, mttr_target_seconds, created_at, updated_at, mttpc_target_seconds)
VALUES ($1, $2, $3, $4, $5, NOW(), NOW(), $6)
ON CONFLICT (service_id, severity_level) DO UPDATE SET
    mttd_target_seconds  = EXCLUDED.mttd_target_seconds,
    mttm_target_seconds  = EXCLUDED.mttm_target_seconds,
    mttr_target_seconds  = EXCLUDED.mttr_target_seconds,
    mttpc_target_seconds = EXCLUDED.mttpc_target_seconds,
    updated_at            = NOW()
RETURNING id;

-- name: DeleteServiceSLA :exec
DELETE FROM service_slas
WHERE service_id = $1 AND severity_level = $2;

-- name: ListServiceSLAsByService :many
SELECT id, service_id, severity_level, mttd_target_seconds, mttm_target_seconds, mttr_target_seconds, created_at, updated_at, mttpc_target_seconds
FROM service_slas
WHERE service_id = $1
ORDER BY severity_level;

-- name: ListServiceSLAsForServices :many
SELECT id, service_id, severity_level, mttd_target_seconds, mttm_target_seconds, mttr_target_seconds, created_at, updated_at, mttpc_target_seconds
FROM service_slas
WHERE service_id = ANY($1::text[]) AND severity_level = $2;
