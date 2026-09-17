package trustvianprocessor_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	trustvianprocessor "trustvian-processor"
)

// noopConsumer discards every batch — used so this benchmark measures
// only this processor's own cost, not a downstream consumer's.
type noopConsumer struct{}

func (noopConsumer) Capabilities() consumer.Capabilities                { return consumer.Capabilities{} }
func (noopConsumer) ConsumeTraces(context.Context, ptrace.Traces) error { return nil }

var _ consumer.Traces = noopConsumer{}

// BenchmarkConsumeTraces measures end-to-end processor throughput —
// span in, mapped, analyzed, enriched, forwarded — for a realistic
// single-span batch (an HTTP server span with a resource and a
// measured duration, the same shape BenchmarkAttributesFromResult and
// BenchmarkEventFromSpan use in the core module). This is the
// "spans/sec under realistic span volume" benchmark task 009 asks for;
// b.N batches are pre-built outside the timed loop so only
// ConsumeTraces itself is measured.
func BenchmarkConsumeTraces(b *testing.B) {
	next := noopConsumer{}
	proc := newTestProcessor(b, next)

	td := buildTraces("svc-payment", 0.95)

	b.ReportAllocs()
	for b.Loop() {
		if err := proc.ConsumeTraces(context.Background(), td); err != nil {
			b.Fatalf("ConsumeTraces() error = %v", err)
		}
	}
}

// BenchmarkConsumeTracesWithMetricsSDK is BenchmarkConsumeTraces with a
// real SDK MeterProvider in place of the Collector's no-op one, so the
// pair measures exactly one variable: what task 043's instrumentation
// costs on the span path.
//
// The no-op case is not a fair "before" on its own — no-op instruments
// short-circuit — so both numbers are needed to say anything honest about
// overhead.
func BenchmarkConsumeTracesWithMetricsSDK(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	set := processor.Settings{
		ID:                component.NewID(component.MustNewType("trustvian")),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}
	set.TelemetrySettings.MeterProvider = provider

	proc, err := trustvianprocessor.NewFactory().CreateTraces(
		context.Background(), set, &trustvianprocessor.Config{}, noopConsumer{})
	if err != nil {
		b.Fatalf("CreateTraces() error = %v", err)
	}
	if err := proc.Start(context.Background(), componenttest.NewNopHost()); err != nil {
		b.Fatalf("Start() error = %v", err)
	}
	b.Cleanup(func() { _ = proc.Shutdown(context.Background()) })

	td := buildTraces("svc-payment", 0.95)

	b.ReportAllocs()
	for b.Loop() {
		if err := proc.ConsumeTraces(context.Background(), td); err != nil {
			b.Fatalf("ConsumeTraces() error = %v", err)
		}
	}
}
