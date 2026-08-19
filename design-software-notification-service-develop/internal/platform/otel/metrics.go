package otel

import "go.opentelemetry.io/otel/metric"

// Metrics holds the RED (rate/errors/duration) instruments plus a notification-delivery
// counter (HU-NOTIF-007). Adapters record into these; the composition root builds one
// instance from the configured MeterProvider and injects it into the adapters that need
// it (in/http, application usecase is NOT one of them -- see doc.go).
type Metrics struct {
	HTTPRequestsTotal      metric.Int64Counter
	HTTPRequestDuration    metric.Float64Histogram
	NotificationsDelivered metric.Int64Counter
}

func NewMetrics(meter metric.Meter) (*Metrics, error) {
	requests, err := meter.Int64Counter("http.server.requests",
		metric.WithDescription("Total HTTP requests handled, by route and status class"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("http.server.request.duration",
		metric.WithDescription("HTTP request duration"), metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	delivered, err := meter.Int64Counter("notification.delivered",
		metric.WithDescription("Notifications delivered, by channel and status"))
	if err != nil {
		return nil, err
	}
	return &Metrics{
		HTTPRequestsTotal:      requests,
		HTTPRequestDuration:    duration,
		NotificationsDelivered: delivered,
	}, nil
}
