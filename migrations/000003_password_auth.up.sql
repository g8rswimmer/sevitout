-- Replace OAuth identity columns with a bcrypt password hash.
ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_oauth_provider_oauth_subject_key,
    DROP COLUMN IF EXISTS oauth_provider,
    DROP COLUMN IF EXISTS oauth_subject,
    ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT '';

-- Remove the default — future inserts must supply the hash explicitly.
ALTER TABLE users ALTER COLUMN password_hash DROP DEFAULT;
