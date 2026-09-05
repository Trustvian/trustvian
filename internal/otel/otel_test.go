package otel_test

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
	trustvianotel "github.com/Trustvian/trustvian/internal/otel"
)

type capturingExporter struct {
	spans []sdktrace.ReadOnlySpan
}

func (e *capturingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *capturingExporter) Shutdown(_ context.Context) error { return nil }

// recordSpan builds a real SDK span and returns the resulting
// sdktrace.ReadOnlySpan. Its interface has an unexported method, so the
// SDK is the only thing that can ever produce one — a hand-written fake
// is not possible, and this is the correct way to test the adapter.
func recordSpan(t testing.TB, res *resource.Resource, kind trace.SpanKind, name string, attrs []attribute.KeyValue, statusErr bool, start, end time.Time) sdktrace.ReadOnlySpan {
	t.Helper()

	exporter := &capturingExporter{}
	opts := []sdktrace.TracerProviderOption{sdktrace.WithSyncer(exporter)}
	if res != nil {
		opts = append(opts, sdktrace.WithResource(res))
	}
	tp := sdktrace.NewTracerProvider(opts...)
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			t.Fatalf("TracerProvider.Shutdown() error = %v", err)
		}
	}()

	tracer := tp.Tracer("trustvian-otel-test")
	_, span := tracer.Start(context.Background(), name,
		trace.WithSpanKind(kind),
		trace.WithAttributes(attrs...),
		trace.WithTimestamp(start),
	)
	if statusErr {
		span.SetStatus(codes.Error, "boom")
	}
	span.End(trace.WithTimestamp(end))

	if len(exporter.spans) != 1 {
		t.Fatalf("captured %d spans, want 1", len(exporter.spans))
	}
	return exporter.spans[0]
}

func testResource(t *testing.T, serviceName, env string) *resource.Resource {
	t.Helper()
	return resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.DeploymentEnvironmentNameKey.String(env),
	)
}

func TestEventFromSpanHTTPServerMapping(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(120 * time.Millisecond)

	span := recordSpan(t,
		testResource(t, "payment-gateway", "production"),
		trace.SpanKindServer,
		"POST /payment",
		[]attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String("POST"),
			semconv.ServerAddressKey.String("payment-svc.internal"),
		},
		false, start, end,
	)

	got := trustvianotel.EventFromSpan(span)

	if got.Operation.Category != event.OperationCategoryHTTP {
		t.Errorf("Operation.Category = %q, want %q", got.Operation.Category, event.OperationCategoryHTTP)
	}
	if got.Operation.Direction != event.DirectionInbound {
		t.Errorf("Operation.Direction = %q, want %q", got.Operation.Direction, event.DirectionInbound)
	}
	if got.Operation.Name != "POST /payment" {
		t.Errorf("Operation.Name = %q, want %q", got.Operation.Name, "POST /payment")
	}
	if got.Actor.ID != "payment-gateway" {
		t.Errorf("Actor.ID = %q, want %q (from resource service.name)", got.Actor.ID, "payment-gateway")
	}
	if got.Actor.Type != event.ActorTypeService {
		t.Errorf("Actor.Type = %q, want %q (default)", got.Actor.Type, event.ActorTypeService)
	}
	if got.Actor.IdentityConfidence != 1.0 {
		t.Errorf("Actor.IdentityConfidence = %v, want 1.0 (default)", got.Actor.IdentityConfidence)
	}
	if got.Context.Environment != "production" {
		t.Errorf("Context.Environment = %q, want %q", got.Context.Environment, "production")
	}
	if got.Target.Name != "payment-svc.internal" {
		t.Errorf("Target.Name = %q, want %q (server.address fallback)", got.Target.Name, "payment-svc.internal")
	}
	if got, want := got.Attributes[features.AttrDurationMS], float64(120); got != want {
		t.Errorf("Attributes[duration_ms] = %v, want %v", got, want)
	}
	if _, ok := got.Attributes[features.AttrError]; ok {
		t.Errorf("Attributes[error] set for a non-error span")
	}
	if got, want := got.Attributes[string(semconv.HTTPRequestMethodKey)], "POST"; got != want {
		t.Errorf("Attributes[http.request.method] = %v, want %v (unmapped attribute passthrough)", got, want)
	}
}

func TestEventFromSpanDBClientMapping(t *testing.T) {
	start := time.Now()
	end := start.Add(5 * time.Millisecond)

	// resource.Empty(), not nil: the SDK's TracerProvider attaches its own
	// default Resource (with a fallback service.name) when none is
	// configured at all, so a genuinely empty one has to be explicit.
	span := recordSpan(t, resource.Empty(), trace.SpanKindClient, "SELECT accounts",
		[]attribute.KeyValue{
			semconv.DBSystemNameKey.String("postgresql"),
			semconv.DBNamespaceKey.String("payment-db"),
		},
		true, start, end,
	)

	got := trustvianotel.EventFromSpan(span)

	if got.Operation.Category != event.OperationCategoryDB {
		t.Errorf("Operation.Category = %q, want %q", got.Operation.Category, event.OperationCategoryDB)
	}
	if got.Operation.Direction != event.DirectionOutbound {
		t.Errorf("Operation.Direction = %q, want %q", got.Operation.Direction, event.DirectionOutbound)
	}
	if got.Target.Name != "payment-db" {
		t.Errorf("Target.Name = %q, want %q (db.namespace)", got.Target.Name, "payment-db")
	}
	if got.Actor.ID != "" {
		t.Errorf("Actor.ID = %q, want empty (no resource, no override — best-effort mapping, not a fabricated value)", got.Actor.ID)
	}
	if v, ok := got.Attributes[features.AttrError].(bool); !ok || !v {
		t.Errorf("Attributes[error] = %v, want true for an error-status span", got.Attributes[features.AttrError])
	}
}

func TestEventFromSpanOverrideAttributes(t *testing.T) {
	start := time.Now()
	span := recordSpan(t, nil, trace.SpanKindInternal, "search_customer",
		[]attribute.KeyValue{
			attribute.String(trustvianotel.AttrActorType, "ai_agent"),
			attribute.String(trustvianotel.AttrActorID, "agent-007"),
			attribute.Float64(trustvianotel.AttrIdentityConfidence, 0.42),
			attribute.String(trustvianotel.AttrOperationCategory, "tool"),
			attribute.String("custom.unmapped", "passthrough-value"),
		},
		false, start, start,
	)

	got := trustvianotel.EventFromSpan(span)

	if got.Actor.Type != event.ActorTypeAIAgent {
		t.Errorf("Actor.Type = %q, want %q", got.Actor.Type, event.ActorTypeAIAgent)
	}
	if got.Actor.ID != "agent-007" {
		t.Errorf("Actor.ID = %q, want %q", got.Actor.ID, "agent-007")
	}
	if got.Actor.IdentityConfidence != 0.42 {
		t.Errorf("Actor.IdentityConfidence = %v, want 0.42", got.Actor.IdentityConfidence)
	}
	if got.Operation.Category != event.OperationCategoryTool {
		t.Errorf("Operation.Category = %q, want %q", got.Operation.Category, event.OperationCategoryTool)
	}
	if got.Operation.Direction != event.DirectionUnspecified {
		t.Errorf("Operation.Direction = %q, want unspecified for SpanKindInternal", got.Operation.Direction)
	}
	if got, want := got.Attributes["custom.unmapped"], "passthrough-value"; got != want {
		t.Errorf(`Attributes["custom.unmapped"] = %v, want %v`, got, want)
	}
	if _, ok := got.Attributes[features.AttrDurationMS]; ok {
		t.Errorf("Attributes[duration_ms] set for a zero-duration span, want absent")
	}
}

func TestEventFromSpanRPCSemconvMapping(t *testing.T) {
	span := recordSpan(t, nil, trace.SpanKindClient, "OrderService.Create",
		[]attribute.KeyValue{semconv.RPCSystemNameKey.String("grpc")},
		false, time.Now(), time.Now(),
	)

	got := trustvianotel.EventFromSpan(span)

	if got.Operation.Category != event.OperationCategoryRPC {
		t.Errorf("Operation.Category = %q, want %q", got.Operation.Category, event.OperationCategoryRPC)
	}
}

func TestEventFromSpanUnclassifiedFallsBackToRPC(t *testing.T) {
	span := recordSpan(t, nil, trace.SpanKindInternal, "do_work", nil, false, time.Now(), time.Now())

	got := trustvianotel.EventFromSpan(span)

	if got.Operation.Category != event.OperationCategoryRPC {
		t.Errorf("Operation.Category = %q, want %q (fallback)", got.Operation.Category, event.OperationCategoryRPC)
	}
}

func TestEventFromSpanIDsAndTimestamp(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	span := recordSpan(t, nil, trace.SpanKindInternal, "op", nil, false, start, start)

	got := trustvianotel.EventFromSpan(span)

	sc := span.SpanContext()
	if got.ID != sc.SpanID().String() {
		t.Errorf("ID = %q, want %q", got.ID, sc.SpanID().String())
	}
	if got.Context.TraceID != sc.TraceID().String() {
		t.Errorf("Context.TraceID = %q, want %q", got.Context.TraceID, sc.TraceID().String())
	}
	if got.Context.SpanID != sc.SpanID().String() {
		t.Errorf("Context.SpanID = %q, want %q", got.Context.SpanID, sc.SpanID().String())
	}
	if !got.Timestamp.Equal(start) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, start)
	}
}

func TestEventFromSpanDeterministic(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	span := recordSpan(t, testResource(t, "svc", "production"), trace.SpanKindServer, "POST /x",
		[]attribute.KeyValue{semconv.HTTPRequestMethodKey.String("POST")},
		false, start, start.Add(10*time.Millisecond),
	)

	first := trustvianotel.EventFromSpan(span)
	second := trustvianotel.EventFromSpan(span)

	if first.ID != second.ID || first.Operation != second.Operation || first.Target != second.Target {
		t.Fatalf("EventFromSpan is not deterministic: first=%+v second=%+v", first, second)
	}
}

// TestEventFromSpanRoundTripsThroughPipeline is the round-trip check: a
// realistic span, once adapted, must be a Validate-passing Event whose
// Attributes carry latency/error signals features.Extract can actually
// see — not just a struct that superficially looks right.
func TestEventFromSpanRoundTripsThroughPipeline(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Millisecond)

	span := recordSpan(t,
		testResource(t, "payment-gateway", "production"),
		trace.SpanKindServer,
		"POST /payment",
		[]attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String("POST"),
			semconv.ServicePeerNameKey.String("checkout-frontend"),
		},
		false, start, end,
	)

	ev := trustvianotel.EventFromSpan(span)

	if err := ev.Validate(); err != nil {
		t.Fatalf("Validate() error = %v for an adapted span that should be a valid Event", err)
	}

	feat := features.Extract(ev)
	if !feat.Volatile.HasLatency {
		t.Fatalf("features.Extract did not see latency from the adapted span's duration_ms attribute")
	}
	if feat.Volatile.Latency != 45*time.Millisecond {
		t.Fatalf("Volatile.Latency = %v, want 45ms", feat.Volatile.Latency)
	}
	if feat.Volatile.Error {
		t.Fatalf("Volatile.Error = true for a non-error span")
	}
	if feat.Stable.TargetName != "checkout-frontend" {
		t.Fatalf("Stable.TargetName = %q, want %q", feat.Stable.TargetName, "checkout-frontend")
	}
}
