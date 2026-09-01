-- Phase 10: per-user integration identity (Slack user ID, GitHub username,
-- Jira account ID) — docs/roadmap.md Phase 10a. Nullable, no uniqueness
-- constraint: a stale/duplicate ID just resolves to the wrong/no invite or
-- assignee, not a data-integrity issue worth enforcing at the DB layer.
ALTER TABLE users ADD COLUMN IF NOT EXISTS slack_user_id TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS github_username TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS jira_account_id TEXT;
