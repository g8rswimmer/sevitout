-- Widen notification_config from one event per rule to many
-- (docs/roadmap.md Phase 15 follow-up): a single rule (role + channel_type +
-- channel_target + max_severity_level) can now match several event types
-- instead of requiring one row per event. TEXT[] + ANY(...) matching mirrors
-- the existing sevs.affected_services / service_slas.service_id precedent
-- elsewhere in this schema — not a new idiom for this codebase.
ALTER TABLE notification_config ADD COLUMN events TEXT[];
UPDATE notification_config SET events = ARRAY[event];
ALTER TABLE notification_config ALTER COLUMN events SET NOT NULL;
ALTER TABLE notification_config ADD CONSTRAINT notification_config_events_nonempty CHECK (array_length(events, 1) > 0);

-- (role, event, channel_type) is no longer a meaningful natural key once a
-- rule can list several events — two rules can legitimately share a role
-- and channel_type while differing only in which events they cover, or even
-- fully overlap. Rules are identified by their existing id column from here
-- on; Upsert/Delete move from a natural-key match to an id match.
ALTER TABLE notification_config DROP CONSTRAINT notification_config_role_event_channel_type_key;
ALTER TABLE notification_config DROP COLUMN event;
