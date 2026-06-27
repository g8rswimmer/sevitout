-- name: InsertAIPlugin :one
INSERT INTO ai_plugins (
    name, version, description, handler_type, http_endpoint,
    provider, model, encrypted_api_key, enabled,
    trigger_on_open, trigger_on_mitigated, trigger_on_resolved, trigger_on_postmortem_review,
    created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING id;

-- name: GetAIPlugin :one
SELECT id, name, version, description, handler_type, http_endpoint,
       provider, model, encrypted_api_key, enabled,
       trigger_on_open, trigger_on_mitigated, trigger_on_resolved, trigger_on_postmortem_review,
       created_at, updated_at
FROM ai_plugins
WHERE id = $1;

-- name: UpdateAIPlugin :exec
UPDATE ai_plugins SET
    name                         = $2,
    version                      = $3,
    description                  = $4,
    handler_type                 = $5,
    http_endpoint                = $6,
    provider                     = $7,
    model                        = $8,
    encrypted_api_key            = $9,
    enabled                      = $10,
    trigger_on_open              = $11,
    trigger_on_mitigated         = $12,
    trigger_on_resolved          = $13,
    trigger_on_postmortem_review = $14,
    updated_at                   = $15
WHERE id = $1;

-- name: DeleteAIPlugin :exec
DELETE FROM ai_plugins WHERE id = $1;

-- name: ListAIPlugins :many
SELECT id, name, version, description, handler_type, http_endpoint,
       provider, model, encrypted_api_key, enabled,
       trigger_on_open, trigger_on_mitigated, trigger_on_resolved, trigger_on_postmortem_review,
       created_at, updated_at
FROM ai_plugins
ORDER BY name;
