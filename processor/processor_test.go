package trustvianprocessor_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	trustvianprocessor "trustvian-processor"
)

// capturingConsumer is the "next" consumer.Traces in the test pipeline:
// it just records every batch it receives, so tests can inspect the
// (now-enriched) spans the processor forwards.
type capturingConsumer struct {
	mu     sync.Mutex
	traces []ptrace.Traces
}

func (c *capturingConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (c *capturingConsumer) ConsumeTraces(_ context.Context, td ptrace.Traces) error {
	c.mu.Lock()
	c.traces = append(c.traces, td)
	c.mu.Unlock()
	return nil
}

func (c *capturingConsumer) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.traces)
}

var _ consumer.Traces = (*capturingConsumer)(nil)

// newTestProcessor builds a real processor.Traces through this
// package's actual factory — the same construction path a Collector
// binary uses — rather than reaching into the unexported
// trustvianProcessor type directly, so the test exercises the real
// component lifecycle (Factory.CreateTraces, Start) a Collector would
// drive.
func newTestProcessor(t testing.TB, next consumer.Traces) processor.Traces {
	t.Helper()
	factory := trustvianprocessor.NewFactory()
	cfg := factory.CreateDefaultConfig()
	set := processor.Settings{
		ID:                component.NewID(component.MustNewType("trustvian")),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}
	proc, err := factory.CreateTraces(context.Background(), set, cfg, next)
	if err != nil {
		t.Fatalf("CreateTraces() error = %v", err)
	}
	if err := proc.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := proc.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	})
	return proc
}

// buildTraces constructs a single-span ptrace.Traces for a given
// actor/operation shape, letting callers vary identityConfidence to
// land on different Trust.Risk buckets (mirrors the core module's
// coldStartEvent helper in internal/otel/attributes_test.go).
func buildTraces(actorID string, identityConfidence float64) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr(string(semconv.ServiceNameKey), actorID)
	rs.Resource().Attributes().PutStr(string(semconv.DeploymentEnvironmentNameKey), "production")

	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("GET /x/" + actorID)
	span.SetKind(ptrace.SpanKindServer)
	now := time.Now()
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(10 * time.Millisecond)))
	span.Attributes().PutDouble("trustvian.identity.confidence", identityConfidence)
	var traceID pcommon.TraceID
	copy(traceID[:], actorID+"0123456789abcdef")
	var spanID pcommon.SpanID
	copy(spanID[:], actorID+"01234567")
	span.SetTraceID(traceID)
	span.SetSpanID(spanID)
	return td
}

func firstSpan(td ptrace.Traces) ptrace.Span {
	return td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
}

// TestConsumeTracesEnrichesSpanAndForwards is the unit test task 009
// asks for: span -> Event -> Result -> enriched span, using the same
// real construction technique (a genuine ptrace.Traces, not a fake)
// internal/otel's own tests established for the core module's span
// type.
func TestConsumeTracesEnrichesSpanAndForwards(t *testing.T) {
	next := &capturingConsumer{}
	proc := newTestProcessor(t, next)

	td := buildTraces("svc-allow", 1.0) // full identity confidence, cold start -> ALLOW-shaped low risk
	if err := proc.ConsumeTraces(context.Background(), td); err != nil {
		t.Fatalf("ConsumeTraces() error = %v", err)
	}

	if got := next.len(); got != 1 {
		t.Fatalf("next consumer received %d batches, want 1", got)
	}

	span := firstSpan(next.traces[0])
	for _, key := range []string{
		"trustvian.anomaly.score",
		"trustvian.trust.score",
		"trustvian.risk.level",
		"trustvian.decision",
		"trustvian.fingerprint.id",
	} {
		if _, ok := span.Attributes().Get(key); !ok {
			t.Errorf("enriched span missing attribute %q", key)
		}
	}

	stats := proc.(interface {
		Stats() trustvianprocessor.Stats
	}).Stats()
	if stats.SpansProcessed != 1 {
		t.Errorf("Stats().SpansProcessed = %d, want 1", stats.SpansProcessed)
	}
	if stats.Invalid != 0 || stats.AnalyzeErrors != 0 {
		t.Errorf("Stats() = %+v, want no invalid/error spans", stats)
	}
	if total := len(stats.Decisions); total == 0 {
		t.Error("Stats().Decisions is empty, want at least one recorded decision")
	}
}

// TestConsumeTracesSkipsInvalidSpanWithoutFailingBatch proves one
// malformed span (here: an out-of-range identity-confidence override,
// which Validate() rejects) doesn't stop ConsumeTraces from returning
// successfully or block forwarding — matching the "must not fail the
// whole batch on one bad span" contract in processor.go's doc comment.
func TestConsumeTracesSkipsInvalidSpanWithoutFailingBatch(t *testing.T) {
	next := &capturingConsumer{}
	proc := newTestProcessor(t, next)

	td := buildTraces("svc-malformed", 5.0) // out of [0,1] -> Validate() rejects
	if err := proc.ConsumeTraces(context.Background(), td); err != nil {
		t.Fatalf("ConsumeTraces() error = %v, want nil (bad span must not fail the batch)", err)
	}

	stats := proc.(interface {
		Stats() trustvianprocessor.Stats
	}).Stats()
	if stats.Invalid != 1 {
		t.Errorf("Stats().Invalid = %d, want 1", stats.Invalid)
	}
	if next.len() != 1 {
		t.Fatalf("next consumer received %d batches, want 1 (still forwarded)", next.len())
	}
	if _, ok := firstSpan(next.traces[0]).Attributes().Get("trustvian.decision"); ok {
		t.Error("an invalid span must not be enriched with trustvian.* attributes")
	}
}

// TestConsumeTracesConcurrent drives many goroutines through one
// processor instance concurrently — the concurrency test task 009
// requires, run under -race (see the Makefile/CI invocation:
// go test ./... -race).
func TestConsumeTracesConcurrent(t *testing.T) {
	next := &capturingConsumer{}
	proc := newTestProcessor(t, next)

	const goroutines = 50
	const perGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(g int) {
			defer wg.Done()
			for i := range perGoroutine {
				actorID := fmt.Sprintf("actor-%d-%d", g, i)
				td := buildTraces(actorID, 0.95)
				if err := proc.ConsumeTraces(context.Background(), td); err != nil {
					t.Errorf("ConsumeTraces() error = %v", err)
				}
			}
		}(g)
	}
	wg.Wait()

	stats := proc.(interface {
		Stats() trustvianprocessor.Stats
	}).Stats()
	want := uint64(goroutines * perGoroutine)
	if stats.SpansProcessed != want {
		t.Errorf("Stats().SpansProcessed = %d, want %d", stats.SpansProcessed, want)
	}
	if got := next.len(); got != int(want) {
		t.Errorf("next consumer received %d batches, want %d", got, want)
	}
}
