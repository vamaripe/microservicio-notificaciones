CREATE INDEX ix_sent_notification_recipient_id
    ON notification.sent_notification (recipient_id);
CREATE INDEX ix_sent_notification_send_status
    ON notification.sent_notification (send_status);
CREATE INDEX ix_sent_notification_source
    ON notification.sent_notification (source_service, source_event_id);
CREATE INDEX ix_sent_notification_template_id
    ON notification.sent_notification (template_id);
