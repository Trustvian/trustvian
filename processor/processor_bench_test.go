package trustvianprocessor_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/ptrace"
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
