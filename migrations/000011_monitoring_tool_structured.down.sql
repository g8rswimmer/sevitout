ALTER TABLE sevs DROP COLUMN IF EXISTS query;
ALTER TABLE sevs RENAME COLUMN dashboard_url TO metric_link;
