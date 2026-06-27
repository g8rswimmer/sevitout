-- name: InsertChatEntry :one
INSERT INTO sev_chat_log (sev_id, occurred_at, source, author, content, added_at, added_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: ListChatEntriesBySEVID :many
SELECT id, sev_id, occurred_at, source, author, content, added_at, added_by
FROM sev_chat_log
WHERE sev_id = $1
ORDER BY occurred_at;
