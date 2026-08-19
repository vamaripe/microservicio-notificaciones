package amqp

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	amqp091 "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/code-sena/design-software-notification-service/api"
	"github.com/code-sena/design-software-notification-service/internal/application/port/in"
	otelplatform "github.com/code-sena/design-software-notification-service/internal/platform/otel"
)

// Topic exchanges this consumer binds to, one per upstream domain (ADR-001 convention:
// "<domain>-events"). Declared here (not just assumed) so the consumer works standalone
// even before scheduling-service/monitoring-service exist as code.
const (
	exchangeScheduling = "scheduling-events"
	exchangeMonitoring = "monitoring-events"

	routingKeySchedulePublished = "scheduling.schedule.published"
	routingKeyAlertTriggered    = "monitoring.alert.triggered"

	queueName = "notification-service.events"
)

// defaultTracer resolves against whatever global TracerProvider is configured at
// process startup (internal/platform/otel.Setup). NOT used by tests: otel's global
// registry only lets the real delegate be assigned ONCE per process
// (sync.Once in go.opentelemetry.io/otel/internal/global), so swapping
// otel.SetTracerProvider between tests would silently stop working after the first
// test that calls it. Tests inject their own tracer directly (Consumer.tracer, an
// unexported field this package's tests can set via struct literal).
var defaultTracer = otel.Tracer("notification-service/adapter/in/amqp")

// propagator is constructed directly (not via otel.GetTextMapPropagator()) so this
// package's own W3C trace-context injection/extraction never depends on global otel
// state having been configured -- same rationale as defaultTracer above, and it
// matches what internal/platform/otel.Setup configures globally anyway.
var propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

// Consumer consumes scheduling.schedule.published and monitoring.alert.triggered from
// RabbitMQ (HU-NOTIF-002) and delegates each decoded envelope to the use case.
type Consumer struct {
	conn   *amqp091.Connection
	ch     *amqp091.Channel
	uc     in.ConsumeDomainEventUseCase
	tracer trace.Tracer
}

// NewConsumer dials amqpURL, declares the topology (exchanges, queue, bindings) and
// returns a Consumer ready for Run. Declarations are idempotent, so this is safe even
// if the exchanges/queue already exist with the same parameters.
func NewConsumer(amqpURL string, uc in.ConsumeDomainEventUseCase) (*Consumer, error) {
	conn, err := amqp091.Dial(amqpURL)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	for _, ex := range []string{exchangeScheduling, exchangeMonitoring} {
		if err := ch.ExchangeDeclare(ex, "topic", true, false, false, false, nil); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, err
		}
	}
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(queueName, routingKeySchedulePublished, exchangeScheduling, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.QueueBind(queueName, routingKeyAlertTriggered, exchangeMonitoring, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}

	return &Consumer{conn: conn, ch: ch, uc: uc, tracer: defaultTracer}, nil
}

// Run consumes until ctx is done. Envelope decode/validation failures (E3: invalid
// envelope) are rejected without requeue -- there is no DLQ yet (that's HU-NOTIF-004),
// so the message is dropped rather than looping forever. Use-case errors after a valid
// envelope (e.g. a transient resolver failure) are logged and acked: formal retry/backoff
// is also HU-NOTIF-004's job, not this one's.
func (c *Consumer) Run(ctx context.Context) error {
	deliveries, err := c.ch.ConsumeWithContext(ctx, queueName, "notification-service", false, false, false, false, nil)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return nil
			}
			c.handle(ctx, d)
		}
	}
}

func (c *Consumer) handle(ctx context.Context, d amqp091.Delivery) {
	var env api.DomainEventEnvelope
	if err := json.Unmarshal(d.Body, &env); err != nil {
		log.Printf("notification-worker: rejecting invalid envelope: %v", err)
		_ = d.Nack(false, false) // no requeue: unparseable message, no DLQ configured yet
		return
	}

	// Extract any upstream traceparent from the AMQP headers (ADR-008 §4: the trace
	// must cross the async hop). If the publisher didn't set one (e.g. no real
	// scheduling/monitoring-service yet), this is a no-op and we start a fresh trace.
	propCtx := propagator.Extract(ctx, otelplatform.AMQPHeaderCarrier(d.Headers))
	spanCtx, span := c.tracer.Start(propCtx, "amqp.consume "+env.EventType,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination", queueName),
			attribute.String("event.type", env.EventType),
			attribute.String("event.id", env.EventID),
		),
	)
	defer span.End()

	// Re-inject the now-active consume span's context so it can be carried in the
	// outbox payload and picked back up later by OutboxRelay when it publishes
	// notification.notification.sent -- otherwise that publish would start a
	// disconnected trace once the consumer's in-memory context is long gone.
	carrier := propagation.MapCarrier{}
	propagator.Inject(spanCtx, carrier)

	err := c.uc.Handle(spanCtx, in.ConsumeDomainEventCommand{
		EventID:       env.EventID,
		EventType:     env.EventType,
		SourceService: env.SourceService,
		Payload:       env.Payload,
		TraceParent:   carrier.Get("traceparent"),
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		log.Printf("notification-worker: failed to process event %s (%s): %v", env.EventID, env.EventType, err)
	}
	_ = d.Ack(false)
}

var errAMQPConnectionClosed = errors.New("amqp connection is closed")

// Ping reports whether the AMQP connection is usable (used by GET /ready).
func (c *Consumer) Ping(_ context.Context) error {
	if c.conn == nil || c.conn.IsClosed() {
		return errAMQPConnectionClosed
	}
	return nil
}

func (c *Consumer) Close() error {
	chErr := c.ch.Close()
	connErr := c.conn.Close()
	if chErr != nil {
		return chErr
	}
	return connErr
}
