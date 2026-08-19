-- Outbox pattern support for notification-service (ADR-001, at-least-once event delivery).
-- Rule: no foreign keys in 03_tables; none needed here (event_id is not a foreign key).

CREATE TABLE notification.outbox (
    id           UUID         NOT NULL DEFAULT gen_random_uuid(),
    event_id     UUID         NOT NULL,
    event_type   VARCHAR(100) NOT NULL,
    payload      JSONB        NOT NULL,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    CONSTRAINT pk_outbox PRIMARY KEY (id),
    CONSTRAINT uq_outbox_event_id UNIQUE (event_id)
);
