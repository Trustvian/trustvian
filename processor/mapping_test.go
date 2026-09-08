package trustvianprocessor_test

import (
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/Trustvian/trustvian/event"
	trustvianprocessor "trustvian-processor"
)

// newTestSpan builds a standalone ptrace.Span with the given kind,
// name, and attributes, and sets start/end timestamps. Unlike
// sdktrace.ReadOnlySpan in the core module, ptrace.Span has no
// unexported-method restriction — pdata types are plain, constructible
// value types, so a hand-built one is the correct way to test this
// mapping, not a workaround.
func newTestSpan(t *testing.T, kind ptrace.SpanKind, name string, attrs map[string]any, statusErr bool, start, end time.Time) ptrace.Span {
	t.Helper()
	span := ptrace.NewSpan()
	span.SetName(name)
	span.SetKind(kind)
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(start))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(end))
	if statusErr {
		span.Status().SetCode(ptrace.StatusCodeError)
	}
	if err := span.Attributes().FromRaw(attrs); err != nil {
		t.Fatalf("Attributes().FromRaw() error = %v", err)
	}
	var traceID pcommon.TraceID
	copy(traceID[:], "0123456789abcdef")
	var spanID pcommon.SpanID
	copy(spanID[:], "01234567")
	span.SetTraceID(traceID)
	span.SetSpanID(spanID)
	return span
}

func newTestResource(t *testing.T, serviceName, env string) pcommon.Map {
	t.Helper()
	m := pcommon.NewMap()
	if serviceName != "" {
		m.PutStr(string(semconv.ServiceNameKey), serviceName)
	}
	if env != "" {
		m.PutStr(string(semconv.DeploymentEnvironmentNameKey), env)
	}
	return m
}

func TestEventFromSpanHTTPServerMapping(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Millisecond)

	span := newTestSpan(t, ptrace.SpanKindServer, "POST /payment", map[string]any{
		string(semconv.HTTPRequestMethodKey): "POST",
		string(semconv.ServicePeerNameKey):   "checkout-frontend",
	}, false, start, end)

	ev := trustvianprocessor.EventFromSpan(newTestResource(t, "payment-gateway", "production"), span)

	if ev.Actor.ID != "payment-gateway" {
		t.Errorf("Actor.ID = %q, want %q", ev.Actor.ID, "payment-gateway")
	}
	if ev.Operation.Category != event.OperationCategoryHTTP {
		t.Errorf("Operation.Category = %q, want %q", ev.Operation.Category, event.OperationCategoryHTTP)
	}
	if ev.Operation.Name != "POST /payment" {
		t.Errorf("Operation.Name = %q, want %q", ev.Operation.Name, "POST /payment")
	}
	if ev.Operation.Direction != event.DirectionInbound {
		t.Errorf("Operation.Direction = %q, want %q", ev.Operation.Direction, event.DirectionInbound)
	}
	if ev.Target.Name != "checkout-frontend" {
		t.Errorf("Target.Name = %q, want %q", ev.Target.Name, "checkout-frontend")
	}
	if ev.Context.Environment != "production" {
		t.Errorf("Context.Environment = %q, want %q", ev.Context.Environment, "production")
	}
	if v, ok := ev.Attributes["error"]; ok {
		t.Errorf("Attributes[error] = %v, want absent for a non-error span", v)
	}
	dur, ok := ev.Attributes["duration_ms"].(float64)
	if !ok || dur != 45 {
		t.Errorf("Attributes[duration_ms] = %v, want 45", ev.Attributes["duration_ms"])
	}
	if err := ev.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestEventFromSpanOverrideAttributes(t *testing.T) {
	span := newTestSpan(t, ptrace.SpanKindInternal, "search_customer", map[string]any{
		"trustvian.actor.type":          "ai_agent",
		"trustvian.actor.id":            "support-agent",
		"trustvian.identity.confidence": 0.9,
		"trustvian.operation.category":  "tool",
	}, false, time.Now(), time.Now().Add(time.Millisecond))

	ev := trustvianprocessor.EventFromSpan(pcommon.NewMap(), span)

	if ev.Actor.Type != event.ActorTypeAIAgent {
		t.Errorf("Actor.Type = %q, want %q", ev.Actor.Type, event.ActorTypeAIAgent)
	}
	if ev.Actor.ID != "support-agent" {
		t.Errorf("Actor.ID = %q, want %q", ev.Actor.ID, "support-agent")
	}
	if ev.Actor.IdentityConfidence != 0.9 {
		t.Errorf("Actor.IdentityConfidence = %v, want 0.9", ev.Actor.IdentityConfidence)
	}
	if ev.Operation.Category != event.OperationCategoryTool {
		t.Errorf("Operation.Category = %q, want %q", ev.Operation.Category, event.OperationCategoryTool)
	}
}

func TestEventFromSpanUnclassifiedFallsBackToRPC(t *testing.T) {
	span := newTestSpan(t, ptrace.SpanKindInternal, "do-a-thing", nil, false, time.Now(), time.Now().Add(time.Millisecond))
	ev := trustvianprocessor.EventFromSpan(pcommon.NewMap(), span)
	if ev.Operation.Category != event.OperationCategoryRPC {
		t.Errorf("Operation.Category = %q, want %q (RPC fallback)", ev.Operation.Category, event.OperationCategoryRPC)
	}
}

func TestEventFromSpanErrorStatus(t *testing.T) {
	span := newTestSpan(t, ptrace.SpanKindClient, "GET /x", nil, true, time.Now(), time.Now().Add(time.Millisecond))
	ev := trustvianprocessor.EventFromSpan(pcommon.NewMap(), span)
	errVal, _ := ev.Attributes["error"].(bool)
	if !errVal {
		t.Errorf("Attributes[error] = %v, want true", ev.Attributes["error"])
	}
}

func TestEventFromSpanUnsetEndTimestampNoLatency(t *testing.T) {
	// A span whose EndTimestamp was never set (the pcommon.Timestamp
	// zero value, Unix epoch 1970) must NOT be treated as having ended
	// at 1970 with a large negative duration — this is exactly the
	// bug the package doc for bridgeVolatileSignals warns against.
	span := ptrace.NewSpan()
	span.SetName("no-end")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	// EndTimestamp left at its zero value deliberately.

	ev := trustvianprocessor.EventFromSpan(pcommon.NewMap(), span)
	if _, ok := ev.Attributes["duration_ms"]; ok {
		t.Errorf("Attributes[duration_ms] = %v, want absent for a span with no EndTimestamp", ev.Attributes["duration_ms"])
	}
}
