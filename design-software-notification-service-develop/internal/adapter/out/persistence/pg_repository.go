package persistence

import (
	"context"
	"errors"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/code-sena/design-software-notification-service/internal/domain/model"
)

// PgNotificationRepository: adapter de salida hacia notification_db (schema `notification`).
type PgNotificationRepository struct {
	pool *pgxpool.Pool
}

func NewPgNotificationRepository(pool *pgxpool.Pool) *PgNotificationRepository {
	return &PgNotificationRepository{pool: pool}
}

// NewPool abre el pool de conexiones contra notification_db. dsn tipo:
// postgres://design_software_app:<pwd>@host:port/design-software-<env>?sslmode=disable
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}

// NewPoolWithTracing is NewPool with pgx queries instrumented via otelpgx (HU-NOTIF-007):
// every query run through the resulting pool becomes a child span of whatever span is
// active in the caller's context. Used by the composition roots (cmd/*); plain NewPool
// stays as-is for callers (mostly tests) that don't care about tracing.
func NewPoolWithTracing(ctx context.Context, dsn string, tp trace.TracerProvider) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer(otelpgx.WithTracerProvider(tp))
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Ping reports whether notification_db is reachable (used by GET /ready).
func (r *PgNotificationRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

// Save inserta en notification.sent_notification (id y created_at los pone la BD
// via DEFAULT gen_random_uuid()/now()) y rellena n con los valores generados.
func (r *PgNotificationRepository) Save(ctx context.Context, n *model.SentNotification) error {
	const q = `
		INSERT INTO notification.sent_notification
			(recipient_id, recipient_email, channel, subject, body_summary, send_status,
			 template_id, source_service, source_event_id)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5,''), $6,
		        NULLIF($7,'')::uuid, NULLIF($8,''), NULLIF($9,'')::uuid)
		RETURNING id::text, created_at`

	return r.pool.QueryRow(ctx, q,
		n.RecipientID,
		n.RecipientEmail,
		string(n.Channel),
		n.Subject,
		n.BodySummary,
		string(n.SendStatus),
		n.TemplateID,
		n.SourceService,
		n.SourceEventID,
	).Scan(&n.ID, &n.CreatedAt)
}

// SaveWithOutbox inserts n (caller-provided ID) and, when evt is non-nil, evt into
// notification.outbox, in one transaction (Outbox pattern, HU-NOTIF-002). Idempotency:
// uq_sent_notification_source_event_id makes the notification insert a no-op for a
// source_event_id already processed; when that happens, no outbox row is written
// either and alreadyProcessed is true.
func (r *PgNotificationRepository) SaveWithOutbox(ctx context.Context, n *model.SentNotification, evt *model.OutboxEvent) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	const insertNotification = `
		INSERT INTO notification.sent_notification
			(id, recipient_id, recipient_email, channel, subject, body_summary, send_status,
			 failure_reason, template_id, source_service, source_event_id, sent_at, created_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, NULLIF($6,''), $7,
		        NULLIF($8,''), NULLIF($9,'')::uuid, NULLIF($10,''), NULLIF($11,'')::uuid, $12, $13)
		ON CONFLICT (source_event_id) WHERE source_event_id IS NOT NULL DO NOTHING`

	tag, err := tx.Exec(ctx, insertNotification,
		n.ID,
		n.RecipientID,
		n.RecipientEmail,
		string(n.Channel),
		n.Subject,
		n.BodySummary,
		string(n.SendStatus),
		n.FailureReason,
		n.TemplateID,
		n.SourceService,
		n.SourceEventID,
		n.SentAt,
		n.CreatedAt,
	)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		// source_event_id already processed: idempotent no-op, nothing else to do.
		return true, tx.Commit(ctx)
	}

	if evt != nil {
		const insertOutbox = `
			INSERT INTO notification.outbox (id, event_id, event_type, payload, created_at)
			VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, $5)`
		if _, err := tx.Exec(ctx, insertOutbox, evt.ID, evt.EventID, evt.EventType, evt.Payload, evt.CreatedAt); err != nil {
			return false, err
		}
	}

	return false, tx.Commit(ctx)
}

// FindByID busca en notification.sent_notification por UUID.
// Devuelve (nil, nil) si no existe la fila; error no-nil solo ante fallas de infraestructura.
func (r *PgNotificationRepository) FindByID(ctx context.Context, id string) (*model.SentNotification, error) {
	const q = `
		SELECT id::text, recipient_id::text, channel, subject, send_status, sent_at
		FROM notification.sent_notification
		WHERE id = $1::uuid`

	n := &model.SentNotification{}
	err := r.pool.QueryRow(ctx, q, id).Scan(
		&n.ID, &n.RecipientID, &n.Channel, &n.Subject, &n.SendStatus, &n.SentAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}
