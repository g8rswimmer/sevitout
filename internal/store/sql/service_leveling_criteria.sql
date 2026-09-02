-- name: GetServiceLevelingCriteria :one
SELECT id, service_id, severity_level, criteria, created_at, updated_at
FROM service_leveling_criteria
WHERE service_id = $1 AND severity_level = $2;

-- name: UpsertServiceLevelingCriteria :one
INSERT INTO service_leveling_criteria (service_id, severity_level, criteria, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (service_id, severity_level) DO UPDATE SET
    criteria   = EXCLUDED.criteria,
    updated_at = NOW()
RETURNING id;

-- name: DeleteServiceLevelingCriteria :exec
DELETE FROM service_leveling_criteria
WHERE service_id = $1 AND severity_level = $2;

-- name: ListServiceLevelingCriteriaByService :many
SELECT id, service_id, severity_level, criteria, created_at, updated_at
FROM service_leveling_criteria
WHERE service_id = $1
ORDER BY severity_level;

-- name: ListServiceLevelingCriteriaForServices :many
SELECT id, service_id, severity_level, criteria, created_at, updated_at
FROM service_leveling_criteria
WHERE service_id = ANY($1::text[]) AND severity_level = $2;
