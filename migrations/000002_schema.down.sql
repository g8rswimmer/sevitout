-- M01: Roll back full application schema

REVOKE INSERT ON audit_log FROM audit_writer;
DROP ROLE IF EXISTS audit_writer;

DROP TRIGGER IF EXISTS tsvector_update_sevs           ON sevs;
DROP TRIGGER IF EXISTS tsvector_update_announcements  ON sev_announcements;
DROP FUNCTION IF EXISTS update_sevs_search_vector();
DROP FUNCTION IF EXISTS update_announcements_search_vector();

DROP TABLE IF EXISTS shareable_links;
DROP TABLE IF EXISTS retention_config;
DROP TABLE IF EXISTS notification_config;
DROP TABLE IF EXISTS integration_config;
DROP TABLE IF EXISTS oncall_rotations;
DROP TABLE IF EXISTS ai_outputs;
DROP TABLE IF EXISTS ai_plugins;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS postmortems;
DROP TABLE IF EXISTS sev_slis;
DROP TABLE IF EXISTS sev_links;
DROP TABLE IF EXISTS sev_linked_tasks;
DROP TABLE IF EXISTS sev_chat_log;
DROP TABLE IF EXISTS sev_announcements;
DROP TABLE IF EXISTS sev_roles;
DROP TABLE IF EXISTS sev_status_history;
DROP TABLE IF EXISTS sevs;
DROP TABLE IF EXISTS services;
DROP TABLE IF EXISTS users;

DROP SEQUENCE IF EXISTS sev_number_seq;
