-- name: InsertAnnouncement :one
INSERT INTO sev_announcements (sev_id, author_id, message, audience, is_milestone, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListAnnouncementsBySEVID :many
SELECT id, sev_id, author_id, message, audience, is_milestone, created_at
FROM sev_announcements
WHERE sev_id = $1
ORDER BY created_at;
