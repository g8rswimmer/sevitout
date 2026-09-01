-- Phase 10f: default assignee on GitHub/Jira issue creation
-- (docs/roadmap.md Phase 10f). Stores the assignee sent at issue-creation
-- time (a GitHub login or a Jira account ID, depending on external_system)
-- so the linked-tasks list can show it without a live re-fetch against the
-- tracker. Free text, unvalidated — mirrors external_system/task_id's own
-- posture. NULL for tasks linked before this shipped, and for tasks linked
-- via plain LinkTask (no assignee concept there).
ALTER TABLE sev_linked_tasks ADD COLUMN IF NOT EXISTS assignee TEXT;
