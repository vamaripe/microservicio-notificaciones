-- Speeds up the relay's poll query (unpublished rows, oldest first).
CREATE INDEX ix_outbox_unpublished
    ON notification.outbox (created_at)
    WHERE published_at IS NULL;
