-- FK del dominio notification (regla de oro: FKs en 04_alter).
ALTER TABLE notification.sent_notification
    ADD CONSTRAINT fk_sent_notification_template
    FOREIGN KEY (template_id) REFERENCES notification.notification_template (id)
    ON UPDATE RESTRICT ON DELETE SET NULL;
