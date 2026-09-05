-- Collapse events[] back to a single event column. Lossy for any rule that
-- had accumulated more than one event under the widened schema: only the
-- first event survives, and re-adding the (role, event, channel_type)
-- unique constraint will fail outright if that collapse produces a
-- duplicate. This is an accepted, documented rollback limitation, not a bug
-- — reversing a genuine widening of the data model can't fully restore the
-- narrower shape without discarding data.
ALTER TABLE notification_config ADD COLUMN event TEXT;
UPDATE notification_config SET event = events[1];
ALTER TABLE notification_config ALTER COLUMN event SET NOT NULL;
ALTER TABLE notification_config DROP CONSTRAINT IF EXISTS notification_config_events_nonempty;
ALTER TABLE notification_config DROP COLUMN events;
ALTER TABLE notification_config ADD CONSTRAINT notification_config_role_event_channel_type_key UNIQUE (role, event, channel_type);
