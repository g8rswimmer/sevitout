-- name: InsertAIOutput :one
INSERT INTO ai_outputs (
    sev_id, plugin_id, trigger_event, action, content, created_at
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListAIOutputsBySEVID :many
SELECT id, sev_id, plugin_id, trigger_event, action, content, created_at
FROM ai_outputs
WHERE sev_id = $1
ORDER BY created_at;
