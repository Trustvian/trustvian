package otel_test

import (
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	trustvianotel "github.com/Trustvian/trustvian/internal/otel"
)

// BenchmarkEventFromSpan measures span-to-Event conversion for a
// representative span shape: an HTTP server span with a resource
// (service.name/deployment.environment.name), a request-method
// attribute, and a measurable duration — mirroring
// TestEventFromSpanHTTPServerMapping's fixture, since that's the shape
// EventFromSpan is actually exercised against elsewhere in this package.
// recordSpan itself (building the span through a real TracerProvider)
// runs once, outside the timed loop; only EventFromSpan is measured.
func BenchmarkEventFromSpan(b *testing.B) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(120 * time.Millisecond)

	res := resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceNameKey.String("payment-gateway"),
		semconv.DeploymentEnvironmentNameKey.String("production"),
	)

	span := recordSpan(b, res, trace.SpanKindServer, "POST /payment",
		[]attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String("POST"),
			semconv.ServerAddressKey.String("payment-svc.internal"),
		},
		false, start, end,
	)

	b.ReportAllocs()
	for b.Loop() {
		_ = trustvianotel.EventFromSpan(span)
	}
}
