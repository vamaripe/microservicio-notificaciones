package out

import (
	"context"

	"github.com/code-sena/design-software-notification-service/internal/domain/model"
)

// NotificationRepository: puerto de salida (lo implementa el adapter de persistencia).
type NotificationRepository interface {
	// Save persiste n y rellena los campos generados por la BD (ID, CreatedAt).
	Save(ctx context.Context, n *model.SentNotification) error

	// SaveWithOutbox persists n and, when evt is non-nil, stages evt in the outbox in
	// the same transaction (Outbox pattern). n and evt must already have their IDs set
	// by the caller. If n.SourceEventID was already processed (idempotency), no row is
	// written for either table and alreadyProcessed is true.
	SaveWithOutbox(ctx context.Context, n *model.SentNotification, evt *model.OutboxEvent) (alreadyProcessed bool, err error)

	// FindByID retrieves a SentNotification by its UUID id.
	// Returns (nil, nil) when no row matches (not found); non-nil error only on
	// unexpected infrastructure failures.
	FindByID(ctx context.Context, id string) (*model.SentNotification, error)
}
