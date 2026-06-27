-- name: GetIntegrationConfig :one
SELECT id, integration_type, encrypted_credentials, settings, created_at, updated_at
FROM integration_config
WHERE integration_type = $1;

-- name: UpsertIntegrationConfig :one
INSERT INTO integration_config (integration_type, encrypted_credentials, settings, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
ON CONFLICT (integration_type) DO UPDATE SET
    encrypted_credentials = EXCLUDED.encrypted_credentials,
    settings              = EXCLUDED.settings,
    updated_at            = NOW()
RETURNING id;

-- name: ListIntegrationConfigs :many
SELECT id, integration_type, encrypted_credentials, settings, created_at, updated_at
FROM integration_config
ORDER BY integration_type;
