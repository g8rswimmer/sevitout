-- name: InsertAnnouncement :one
INSERT INTO sev_announcements (sev_id, author_id, message, audience, is_milestone, created_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ListAnnouncementsBySEVID :many
SELECT id, sev_id, author_id, message, audience, is_milestone, created_at
FROM sev_announcements
WHERE sev_id = $1
ORDER BY created_at;

-- name: SearchAnnouncementSEVIDs :many
-- search_vector is a tsvector populated by the tsvector_update_announcements
-- trigger (migration 000002); plainto_tsquery tokenizes free-form input
-- (implicit AND between words) the same way buildSEVFilterWhere's own
-- search-vector query does, so it can't produce a malformed query.
SELECT DISTINCT sev_id FROM sev_announcements
WHERE search_vector @@ plainto_tsquery('english', $1::text);
