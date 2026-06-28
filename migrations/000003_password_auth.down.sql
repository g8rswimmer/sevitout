-- Restore OAuth columns and remove password_hash.
ALTER TABLE users
    DROP COLUMN IF EXISTS password_hash,
    ADD COLUMN IF NOT EXISTS oauth_provider TEXT NOT NULL DEFAULT 'local',
    ADD COLUMN IF NOT EXISTS oauth_subject  TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT users_oauth_provider_oauth_subject_key UNIQUE (oauth_provider, oauth_subject);
