-- name: InsertSEV :exec
INSERT INTO sevs (
    id, title, description, severity_level, status,
    root_cause_category, root_cause_description, mitigation, prevention,
    business_impact, affected_services, detection_method, alert_name,
    monitoring_tool, alert_url, dashboard_url, query, snapshot_url, github_repo,
    root_cause_reference_url,
    right_people_present, right_people_notes, tags,
    started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
    mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
    locked, sensitive, ai_disabled, created_at, updated_at, created_by,
    slack_channel_id, mttpc_seconds
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19,
    $20,
    $21, $22, $23,
    $24, $25, $26, $27, $28,
    $29, $30, $31, $32,
    $33, $34, $35, $36, $37, $38,
    $39, $40
);

-- name: GetSEV :one
SELECT id, title, description, severity_level, status,
       root_cause_category, root_cause_description, mitigation, prevention,
       business_impact, affected_services, detection_method, alert_name,
       monitoring_tool, alert_url, dashboard_url, query, snapshot_url, github_repo,
       root_cause_reference_url,
       right_people_present, right_people_notes, tags,
       started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
       mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
       locked, sensitive, ai_disabled, created_at, updated_at, created_by,
       slack_channel_id, mttpc_seconds
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
    alert_url              = $15,
    dashboard_url          = $16,
    query                  = $17,
    snapshot_url           = $18,
    github_repo            = $19,
    root_cause_reference_url = $20,
    right_people_present   = $21,
    right_people_notes     = $22,
    tags                   = $23,
    started_at             = $24,
    detected_at            = $25,
    mitigated_at           = $26,
    resolved_at            = $27,
    postmortem_completed_at = $28,
    mttd_seconds           = $29,
    mttm_seconds           = $30,
    mttr_seconds           = $31,
    dttm_seconds           = $32,
    locked                 = $33,
    sensitive              = $34,
    ai_disabled            = $35,
    updated_at             = $36,
    slack_channel_id       = $37,
    mttpc_seconds          = $38
WHERE id = $1;

-- name: UpdateSEVLocked :exec
UPDATE sevs SET locked = $2, updated_at = NOW() WHERE id = $1;

-- name: ListSEVs :many
SELECT id, title, description, severity_level, status,
       root_cause_category, root_cause_description, mitigation, prevention,
       business_impact, affected_services, detection_method, alert_name,
       monitoring_tool, alert_url, dashboard_url, query, snapshot_url, github_repo,
       root_cause_reference_url,
       right_people_present, right_people_notes, tags,
       started_at, detected_at, mitigated_at, resolved_at, postmortem_completed_at,
       mttd_seconds, mttm_seconds, mttr_seconds, dttm_seconds,
       locked, sensitive, ai_disabled, created_at, updated_at, created_by,
       slack_channel_id, mttpc_seconds
FROM sevs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: NextSEVNumber :one
SELECT nextval('sev_number_seq')::bigint AS seq;
