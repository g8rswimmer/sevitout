-- The in-memory TaskStore has always rejected a duplicate (sev_id,
-- external_system, task_id) triple with ErrConflict (the same external task
-- can't be linked to the same SEV twice), but this constraint was never
-- added to the schema, so the PostgreSQL-backed store silently allowed
-- duplicates. Add it so both implementations enforce the same rule.
ALTER TABLE sev_linked_tasks
    ADD CONSTRAINT sev_linked_tasks_sev_external_task_key
    UNIQUE (sev_id, external_system, task_id);
