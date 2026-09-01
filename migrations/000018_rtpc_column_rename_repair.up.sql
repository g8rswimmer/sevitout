-- Repairs a schema-drift bug: migration 000017 was renamed in place (from
-- "mttpc" to "rtpc" — the metric itself was renamed from MTTPC to RTPC and
-- its baseline corrected from mitigated_at to resolved_at) before ever being
-- released, but golang-migrate tracks applied migrations by version number
-- only (17), not file content — so any database that had already run
-- version 17 under its old content kept the old mttpc_* column names
-- forever, silently diverging from what the application code (which only
-- ever knows about rtpc_*) expects. Every SEV read/write then fails with
-- "column \"rtpc_seconds\" does not exist".
--
-- This migration is a no-op on a fresh database (000017 already creates the
-- rtpc_* columns directly, so the IF EXISTS guards below never fire) and
-- repairs exactly the drifted case otherwise.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'sevs' AND column_name = 'mttpc_seconds'
    ) THEN
        ALTER TABLE sevs RENAME COLUMN mttpc_seconds TO rtpc_seconds;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'service_slas' AND column_name = 'mttpc_target_seconds'
    ) THEN
        ALTER TABLE service_slas RENAME COLUMN mttpc_target_seconds TO rtpc_target_seconds;
    END IF;
END $$;
