-- Idempotent inbound event consumption (HU-NOTIF-002): a domain event's source_event_id
-- must map to at most one sent_notification row. Partial (NULLs allowed) because
-- source_event_id is optional for notifications created via the synchronous API (HU-NOTIF-001).
CREATE UNIQUE INDEX uq_sent_notification_source_event_id
    ON notification.sent_notification (source_event_id)
    WHERE source_event_id IS NOT NULL;
