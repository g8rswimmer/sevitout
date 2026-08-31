-- Phase 10e: persist the SEV -> Slack incident-channel mapping server-side
-- (docs/roadmap.md Phase 10e), closing the in-memory-only limitation noted
-- in demo/M11-slack-bot.md. Only SEVs whose incident channel was created
-- after this ships get a value here — older SEVs stay NULL, and the "Add to
-- chat" action must treat that as disabled, not an error.
ALTER TABLE sevs ADD COLUMN IF NOT EXISTS slack_channel_id TEXT;
