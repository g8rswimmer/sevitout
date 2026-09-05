ALTER TABLE sevs DROP COLUMN IF EXISTS escalated_at;
DROP TABLE IF EXISTS escalation_config;
ALTER TABLE notification_config DROP COLUMN IF EXISTS max_severity_level;
