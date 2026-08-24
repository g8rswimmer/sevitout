-- 000002 granted audit_writer INSERT on audit_log, but audit_log.id is
-- BIGSERIAL: inserting into it calls nextval() on the backing sequence,
-- which is a separate, unrelated privilege in Postgres — GRANT INSERT ON
-- the table alone does not cover it. Without this, every INSERT as
-- audit_writer failed with "permission denied for sequence
-- audit_log_id_seq", which went unnoticed until this role was actually
-- exercised end-to-end (TestAuditWriterRole) against a real database.
GRANT USAGE ON SEQUENCE audit_log_id_seq TO audit_writer;
