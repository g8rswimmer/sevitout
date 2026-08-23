-- M12: AI plugin system

-- Per-SEV override to disable AI entirely for a specific incident (§11.3),
-- e.g. sensitive SEVs that also want to suppress AI-generated content.
ALTER TABLE sevs ADD COLUMN IF NOT EXISTS ai_disabled BOOLEAN NOT NULL DEFAULT FALSE;

-- Per-plugin rate limit, enforced by the dispatcher (§11.3 "rate limiting and
-- quota controls"). 0 means unlimited.
ALTER TABLE ai_plugins ADD COLUMN IF NOT EXISTS rate_limit_per_minute INT NOT NULL DEFAULT 10;
