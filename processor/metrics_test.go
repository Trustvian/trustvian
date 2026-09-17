package trustvianprocessor_test

// Task 043's proof that the processor's operational metrics are recorded
// on the real span path — through factory.CreateTraces and ConsumeTraces,
// not by calling the metrics package directly. The metrics package's own
// tests prove each instrument in isolation; these prove the wiring, which
// is the part a refactor can silently remove.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	trustvianprocessor "trustvian-processor"
	tvmetrics "trustvian-processor/internal/metrics"
)

// newMeteredProcessor builds a processor whose TelemetrySettings carry a
// real SDK MeterProvider backed by an in-memory reader, which is exactly
// how the Collector injects one — the processor has no other source of a
// meter, so this is the real path and not a test-only hook.
func newMeteredProcessor(t *testing.T, cfg *trustvianprocessor.Config) (processor.Traces, func() metricdata.ResourceMetrics) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	set := processor.Settings{
		ID:                component.NewID(component.MustNewType("trustvian")),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}
	set.TelemetrySettings.MeterProvider = provider

	proc, err := trustvianprocessor.NewFactory().CreateTraces(
		context.Background(), set, cfg, &capturingConsumer{})
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

	return proc, func() metricdata.ResourceMetrics {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		return rm
	}
}

// outcomes returns counter name → outcome/decision value → count for the
// whole collected set, which is what these tests actually assert on.
func outcomes(t *testing.T, rm metricdata.ResourceMetrics) map[string]map[string]int64 {
	t.Helper()

	out := map[string]map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		if sm.Scope.Name != tvmetrics.ScopeName {
			continue
		}
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if out[m.Name] == nil {
					out[m.Name] = map[string]int64{}
				}
				for _, kv := range dp.Attributes.ToSlice() {
					out[m.Name][kv.Value.Emit()] += dp.Value
				}
			}
		}
	}
	return out
}

func histogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string) uint64 {
	t.Helper()

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s is %T, want a float64 histogram", name, m.Data)
			}
			var total uint64
			for _, dp := range hist.DataPoints {
				total += dp.Count
			}
			return total
		}
	}
	return 0
}

// TestMetricsRecordedOnRealSpanPath is the wiring test: a span that maps
// to a valid Event must move every one of the five instruments.
func TestMetricsRecordedOnRealSpanPath(t *testing.T) {
	proc, gather := newMeteredProcessor(t, &trustvianprocessor.Config{})

	if err := proc.ConsumeTraces(context.Background(), buildTraces("svc-metrics", 0.95)); err != nil {
		t.Fatalf("ConsumeTraces() error = %v", err)
	}

	rm := gather()
	counts := outcomes(t, rm)

	if got := counts["trustvian.analyses"][tvmetrics.OutcomeAnalyzed]; got != 1 {
		t.Errorf("analyses{analyzed} = %d, want 1 — the span path is not recording analyses", got)
	}
	if total := sumValues(counts["trustvian.decisions"]); total != 1 {
		t.Errorf("decisions total = %d, want 1: %v", total, counts["trustvian.decisions"])
	}
	if total := sumValues(counts["trustvian.observations"]); total != 1 {
		t.Errorf("observations total = %d, want 1: %v", total, counts["trustvian.observations"])
	}
	if got := histogramCount(t, rm, "trustvian.analysis.duration"); got != 1 {
		t.Errorf("analysis.duration count = %d, want 1", got)
	}
	if got := histogramCount(t, rm, "trustvian.observe.duration"); got != 1 {
		t.Errorf("observe.duration count = %d, want 1", got)
	}
}

// TestInvalidSpanRecordsInvalidEventOnly pins the outcome split: a span
// that never becomes a valid Event is counted as invalid_event and must
// not produce a decision, an observation, or a latency sample — recording
// a duration for work that never ran would corrupt the latency histogram.
func TestInvalidSpanRecordsInvalidEventOnly(t *testing.T) {
	proc, gather := newMeteredProcessor(t, &trustvianprocessor.Config{})

	// No service.name resource attribute, so EventFromSpan cannot produce
	// an Actor and Validate fails.
	td := ptrace.NewTraces()
	span := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("orphan")
	span.SetKind(ptrace.SpanKindServer)
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Now()))

	if err := proc.ConsumeTraces(context.Background(), td); err != nil {
		t.Fatalf("ConsumeTraces() error = %v", err)
	}

	rm := gather()
	counts := outcomes(t, rm)

	if got := counts["trustvian.analyses"][tvmetrics.OutcomeInvalidEvent]; got != 1 {
		t.Errorf("analyses{invalid_event} = %d, want 1: %v", got, counts["trustvian.analyses"])
	}
	if got := sumValues(counts["trustvian.decisions"]); got != 0 {
		t.Errorf("decisions = %d, want 0 — an invalid span produced no decision", got)
	}
	if got := sumValues(counts["trustvian.observations"]); got != 0 {
		t.Errorf("observations = %d, want 0 — an invalid span reached no store", got)
	}
	if got := histogramCount(t, rm, "trustvian.analysis.duration"); got != 0 {
		t.Errorf("analysis.duration count = %d, want 0 — no analysis ran", got)
	}
}

// TestObserveErrorRecordsBoundedCategory forces a real error out of the
// store, on the real span path, and proves the error is counted as a
// category with no error text attached.
//
// A file-backed store whose directory is made unwritable is the
// DB-free way to do this: FileStore.Observe rewrites the file on every
// call, so the flush fails and Engine.Observe returns a wrapped *os
// error whose text contains a filesystem path — precisely the kind of
// string that must never become a label.
func TestObserveErrorRecordsBoundedCategory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod cannot make a directory unwritable")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "baselines.json")

	proc, gather := newMeteredProcessor(t, &trustvianprocessor.Config{
		Storage: map[string]any{
			"version": "v1",
			"type":    "file",
			"file":    map[string]any{"path": path},
		},
	})

	// Revoke write permission after construction: the store must exist
	// (fail-closed startup would otherwise reject the config) and only
	// then lose its ability to flush.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := proc.ConsumeTraces(context.Background(), buildTraces("svc-observe-err", 0.95)); err != nil {
		t.Fatalf("ConsumeTraces() error = %v", err)
	}

	rm := gather()
	counts := outcomes(t, rm)

	if got := counts["trustvian.observations"][tvmetrics.OutcomeError]; got != 1 {
		t.Fatalf("observations{error} = %d, want 1: %v", got, counts["trustvian.observations"])
	}
	// The analysis itself succeeded — a store write failure must not be
	// reported as a failed analysis.
	if got := counts["trustvian.analyses"][tvmetrics.OutcomeAnalyzed]; got != 1 {
		t.Errorf("analyses{analyzed} = %d, want 1: %v", got, counts["trustvian.analyses"])
	}

	// And no attribute anywhere carries the path the error text contains.
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				for _, kv := range dp.Attributes.ToSlice() {
					if v := kv.Value.Emit(); containsAny(v, dir, path, "permission denied") {
						t.Errorf("metric %q attribute %q leaked error detail: %q", m.Name, kv.Key, v)
					}
				}
			}
		}
	}
}

// TestMetricsUnderConcurrentSpans runs the real path from many goroutines
// at once: the counters must be race-free and must add up exactly.
func TestMetricsUnderConcurrentSpans(t *testing.T) {
	proc, gather := newMeteredProcessor(t, &trustvianprocessor.Config{})

	const goroutines, perGoroutine = 8, 25

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perGoroutine {
				td := buildTraces(actorName(g, i), 0.95)
				if err := proc.ConsumeTraces(context.Background(), td); err != nil {
					t.Errorf("ConsumeTraces() error = %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	counts := outcomes(t, gather())
	const want = goroutines * perGoroutine

	if got := counts["trustvian.analyses"][tvmetrics.OutcomeAnalyzed]; got != want {
		t.Errorf("analyses{analyzed} = %d, want %d — aggregation lost or double-counted", got, want)
	}
	if got := sumValues(counts["trustvian.decisions"]); got != want {
		t.Errorf("decisions total = %d, want %d", got, want)
	}
	if got := sumValues(counts["trustvian.observations"]); got != want {
		t.Errorf("observations total = %d, want %d", got, want)
	}

	// Cardinality must not grow with traffic: that is the whole property
	// this task's attribute design exists to guarantee.
	if n := len(counts["trustvian.decisions"]); n > 6 {
		t.Errorf("decisions has %d distinct values after %d spans, want at most 6", n, want)
	}
}

func actorName(g, i int) string {
	return fmt.Sprintf("svc-conc-%d-%d", g, i)
}

func sumValues(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
}
