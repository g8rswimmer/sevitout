-- Follow-up to Phase 12: an SLA target for the postmortem tail, not just
-- incident response — "resolution to postmortem complete" (RTPC), the same
-- point-A-to-point-B shape as dttm_seconds (mitigated_at - detected_at),
-- not "from started_at" like mttd/mttm/mttr_seconds. Measured from
-- resolved_at, not mitigated_at, since the postmortem clock is conventionally
-- understood to start once the incident itself is resolved.
ALTER TABLE sevs ADD COLUMN IF NOT EXISTS rtpc_seconds BIGINT;
ALTER TABLE service_slas ADD COLUMN IF NOT EXISTS rtpc_target_seconds BIGINT;
