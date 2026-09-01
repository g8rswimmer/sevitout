-- Follow-up to Phase 12: an SLA target for the postmortem tail, not just
-- incident response — "mitigation to postmortem complete" (MTTPC), the same
-- point-A-to-point-B shape as dttm_seconds (mitigated_at - detected_at),
-- not "from started_at" like mttd/mttm/mttr_seconds.
ALTER TABLE sevs ADD COLUMN IF NOT EXISTS mttpc_seconds BIGINT;
ALTER TABLE service_slas ADD COLUMN IF NOT EXISTS mttpc_target_seconds BIGINT;
