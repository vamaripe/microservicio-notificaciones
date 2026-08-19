-- Dominio notification (capacidad transversal de entrega, ADR-005).
-- Regla de oro: tablas SIN llaves foraneas; las FK van en 04_alter.

CREATE TABLE notification.notification_template (
    id               UUID        NOT NULL DEFAULT gen_random_uuid(),
    code             VARCHAR(50) NOT NULL,
    name             VARCHAR(200) NOT NULL,
    channel          VARCHAR(20) NOT NULL,
    subject_template VARCHAR(300) NOT NULL,
    body_template    TEXT        NOT NULL,
    is_active        BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_notification_template PRIMARY KEY (id),
    CONSTRAINT uq_notification_template_code UNIQUE (code),
    CONSTRAINT ck_notification_template_channel CHECK (channel IN ('EMAIL','IN_APP'))
);

CREATE TABLE notification.sent_notification (
    id              UUID        NOT NULL DEFAULT gen_random_uuid(),
    recipient_id    UUID        NOT NULL,
    recipient_email VARCHAR(255) NOT NULL,
    channel         VARCHAR(20) NOT NULL,
    subject         VARCHAR(300) NOT NULL,
    body_summary    TEXT,
    send_status     VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    failure_reason  TEXT,
    template_id     UUID,
    source_service  VARCHAR(50),
    source_event_id UUID,
    sent_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT pk_sent_notification PRIMARY KEY (id),
    CONSTRAINT ck_sent_notification_channel CHECK (channel IN ('EMAIL','IN_APP')),
    CONSTRAINT ck_sent_notification_status CHECK (send_status IN ('PENDING','SENT','FAILED'))
);
