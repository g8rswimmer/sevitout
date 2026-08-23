-- name: InsertSEV :exec
INSERT INTO sevs (
    id, title, description, severity_level, status,
    root_cause_category, root_cause_description, mitigation, prevention,
    business_impact, affected_services, detection_method, alert_name,
    monitoring_tool, right_people_present, right_people_notes, tags,
    started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
    mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
    locked, sensitive, ai_disabled, created_at, updated_at, created_by
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15, $16, $17,
    $18, $19, $20, $21, $22,
    $23, $24, $25, $26,
    $27, $28, $29, $30, $31, $32
);

-- name: GetSEV :one
SELECT id, title, description, severity_level, status,
       root_cause_category, root_cause_description, mitigation, prevention,
       business_impact, affected_services, detection_method, alert_name,
       monitoring_tool, right_people_present, right_people_notes, tags,
       started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
       mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
       locked, sensitive, ai_disabled, created_at, updated_at, created_by
FROM sevs
WHERE id = $1;

-- name: UpdateSEV :exec
UPDATE sevs SET
    title                  = $2,
    description            = $3,
    severity_level         = $4,
    status                 = $5,
    root_cause_category    = $6,
    root_cause_description = $7,
    mitigation             = $8,
    prevention             = $9,
    business_impact        = $10,
    affected_services      = $11,
    detection_method       = $12,
    alert_name             = $13,
    monitoring_tool        = $14,
    right_people_present   = $15,
    right_people_notes     = $16,
    tags                   = $17,
    started_at             = $18,
    detected_at            = $19,
    mitigated_at           = $20,
    resolved_at            = $21,
    postmortem_completed_at = $22,
    mttd_seconds           = $23,
    mttm_seconds           = $24,
    mttr_seconds           = $25,
    dttm_seconds           = $26,
    locked                 = $27,
    sensitive              = $28,
    ai_disabled            = $29,
    updated_at             = $30
WHERE id = $1;

-- name: UpdateSEVLocked :exec
UPDATE sevs SET locked = $2, updated_at = NOW() WHERE id = $1;

-- name: ListSEVs :many
SELECT id, title, description, severity_level, status,
       root_cause_category, root_cause_description, mitigation, prevention,
       business_impact, affected_services, detection_method, alert_name,
       monitoring_tool, right_people_present, right_people_notes, tags,
       started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
       mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
       locked, sensitive, ai_disabled, created_at, updated_at, created_by
FROM sevs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: NextSEVNumber :one
SELECT nextval('sev_number_seq')::bigint AS seq;
