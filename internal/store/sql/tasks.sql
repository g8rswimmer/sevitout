-- name: InsertLinkedTask :one
INSERT INTO sev_linked_tasks (
    sev_id, external_system, task_id, url, title, description,
    relationship_type, priority, due_date, overdue, created_at, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING id;

-- name: GetLinkedTask :one
SELECT id, sev_id, external_system, task_id, url, title, description,
       relationship_type, priority, due_date, overdue, created_at, created_by
FROM sev_linked_tasks
WHERE id = $1;

-- name: UpdateLinkedTask :exec
UPDATE sev_linked_tasks SET
    title             = $2,
    description       = $3,
    relationship_type = $4,
    priority          = $5,
    due_date          = $6,
    overdue           = $7
WHERE id = $1;

-- name: DeleteLinkedTask :exec
DELETE FROM sev_linked_tasks WHERE id = $1;

-- name: ListLinkedTasksBySEVID :many
SELECT id, sev_id, external_system, task_id, url, title, description,
       relationship_type, priority, due_date, overdue, created_at, created_by
FROM sev_linked_tasks
WHERE sev_id = $1
ORDER BY created_at;

-- name: SetTaskDueDateIfUnset :execrows
UPDATE sev_linked_tasks SET due_date = $2
WHERE id = $1 AND due_date IS NULL;

-- name: CountOverdueTasks :one
-- due_date is a DATE (no time-of-day), so "due before today" is the closest
-- available match to the in-memory store's due_date.Before(now) semantics.
SELECT COUNT(*) FROM sev_linked_tasks
WHERE due_date IS NOT NULL AND due_date < $1::date;
