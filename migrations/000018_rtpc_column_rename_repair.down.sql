DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'sevs' AND column_name = 'rtpc_seconds'
    ) THEN
        ALTER TABLE sevs RENAME COLUMN rtpc_seconds TO mttpc_seconds;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'service_slas' AND column_name = 'rtpc_target_seconds'
    ) THEN
        ALTER TABLE service_slas RENAME COLUMN rtpc_target_seconds TO mttpc_target_seconds;
    END IF;
END $$;
