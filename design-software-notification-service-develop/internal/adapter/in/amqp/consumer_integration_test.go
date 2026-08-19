//go:build integration

package amqp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/google/uuid"

	"github.com/code-sena/design-software-notification-service/api"
	"github.com/code-sena/design-software-notification-service/internal/adapter/out/client"
	"github.com/code-sena/design-software-notification-service/internal/adapter/out/notifier"
	"github.com/code-sena/design-software-notification-service/internal/adapter/out/persistence"
	"github.com/code-sena/design-software-notification-service/internal/application/usecase"
)

// TestConsumer_MonitoringAlertTriggered_Integration exercises the full HU-NOTIF-002 path
// against real infra: publishes a monitoring.alert.triggered envelope to RabbitMQ, lets
// the real Consumer/use case/pgx repository handle it, and checks the resulting rows.
//
// KNOWN BLOCKED as of this PR (documented, not a code bug):
//  1. notification.outbox does not exist locally yet -- that migration
//     (design-software-notification-db PR, "outbox table + idempotency index") is
//     pending review/apply on the infra side. SaveWithOutbox's transaction will fail on
//     the outbox INSERT and roll back everything, including the sent_notification row.
//  2. MailHog's SMTP port (1025) is only published inside docker-infra's network
//     (mailhog:1025), not to the host. This test runs from the host, so
//     NOTIFICATION_SMTP_ADDR=localhost:1025 will fail to connect until that port is
//     exposed on the docker-infra side.
//
// Both are flagged in the HU-NOTIF-002 report; once fixed, this test should go green
// with no code changes.
func TestConsumer_MonitoringAlertTriggered_Integration(t *testing.T) {
	dbDSN := os.Getenv("NOTIFICATION_DB_DSN")
	amqpURL := os.Getenv("NOTIFICATION_AMQP_URL")
	if dbDSN == "" || amqpURL == "" {
		t.Skip("NOTIFICATION_DB_DSN / NOTIFICATION_AMQP_URL not set; skipping integration test")
	}
	smtpAddr := envOrDefault("NOTIFICATION_SMTP_ADDR", "localhost:1025")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := persistence.NewPool(ctx, dbDSN)
	if err != nil {
		t.Fatalf("failed to open notification_db pool: %v", err)
	}
	defer pool.Close()

	repo := persistence.NewPgNotificationRepository(pool)
	resolver := client.NewStubRecipientResolver("integration-test@sena.local")
	notify := notifier.NewCompositeNotifier(
		notifier.NewSMTPNotifier(smtpAddr, "notifications@sena.local"),
		notifier.NewInAppNotifier(),
		nil, // metrics: not under test here
	)
	uc := usecase.NewConsumeDomainEvent(resolver, notify, repo)

	consumer, err := NewConsumer(amqpURL, uc)
	if err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}
	defer consumer.Close()

	consumeCtx, stopConsuming := context.WithCancel(ctx)
	defer stopConsuming()
	go func() { _ = consumer.Run(consumeCtx) }()

	eventID := uuid.NewString() // source_event_id is a UUID column
	publishAlertTriggered(t, amqpURL, eventID)

	// Poll notification_db for the resulting row (the consumer runs asynchronously).
	deadline := time.Now().Add(10 * time.Second)
	var (
		gotStatus string
		gotID     string
	)
	for time.Now().Before(deadline) {
		err := pool.QueryRow(ctx,
			`SELECT id::text, send_status FROM notification.sent_notification WHERE source_event_id = $1::uuid`,
			eventID,
		).Scan(&gotID, &gotStatus)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if gotID == "" {
		t.Fatalf("no sent_notification row for source_event_id=%s within the deadline (outbox table missing and/or MailHog SMTP unreachable from host -- see test doc comment)", eventID)
	}
	if gotStatus != "SENT" {
		t.Fatalf("send_status = %q, want SENT", gotStatus)
	}

	var outboxCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM notification.outbox WHERE event_type = 'notification.notification.sent' AND payload->>'notification_id' = $1`,
		gotID,
	).Scan(&outboxCount); err != nil {
		t.Fatalf("failed to query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("want 1 outbox row staged for notification %s, got %d", gotID, outboxCount)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM notification.sent_notification WHERE id = $1::uuid`, gotID)
	_, _ = pool.Exec(ctx, `DELETE FROM notification.outbox WHERE payload->>'notification_id' = $1`, gotID)
}

func publishAlertTriggered(t *testing.T, amqpURL, eventID string) {
	t.Helper()
	conn, err := amqp091.Dial(amqpURL)
	if err != nil {
		t.Fatalf("failed to dial amqp for the test publisher: %v", err)
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("failed to open amqp channel for the test publisher: %v", err)
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare(exchangeMonitoring, "topic", true, false, false, false, nil); err != nil {
		t.Fatalf("failed to declare %s: %v", exchangeMonitoring, err)
	}

	env := api.DomainEventEnvelope{
		EventID:       eventID,
		EventType:     routingKeyAlertTriggered,
		Version:       "1.0",
		Timestamp:     time.Now(),
		SourceService: "monitoring-service",
		Payload: map[string]interface{}{
			"affected_entity_type": "Learner",
			"affected_entity_id":   "10101010-1010-1010-1010-101010101010", // real events carry a UUID here
			"alert_type_code":      "LOW_ATTENDANCE",
		},
	}
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal test envelope: %v", err)
	}
	if err := ch.PublishWithContext(context.Background(), exchangeMonitoring, routingKeyAlertTriggered, false, false, amqp091.Publishing{
		ContentType: "application/json",
		Body:        body,
	}); err != nil {
		t.Fatalf("failed to publish test envelope: %v", err)
	}
}

func envOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
