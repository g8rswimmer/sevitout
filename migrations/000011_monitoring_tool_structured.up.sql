-- Phase 6b: structured monitoring-tool metadata (docs/requirements.md §13.4).
--
-- metric_link becomes dashboard_url — its doc comment already described it as
-- "the monitoring dashboard, metric, or saved query" link; that was really
-- two distinct concepts sharing one column. A rename (not drop+add) keeps any
-- existing links intact.
ALTER TABLE sevs RENAME COLUMN metric_link TO dashboard_url;

-- query holds a saved query/expression string (e.g. a PromQL or Datadog
-- query) as a genuinely separate concept from dashboard_url — a query isn't
-- a URL, so it doesn't belong crammed into the same field.
ALTER TABLE sevs ADD COLUMN IF NOT EXISTS query TEXT;

-- monitoring_tool itself stays a plain TEXT column with no CHECK constraint,
-- consistent with detection_method (see migrations/000002_schema.up.sql) —
-- the closed vocabulary (datadog/prometheus/cloudwatch/other) is validated
-- application-side in internal/api/grpc/sev.go, not at the DB layer.
