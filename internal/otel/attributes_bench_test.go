package otel_test

import (
	"context"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
	trustvianotel "github.com/Trustvian/trustvian/internal/otel"
)

// BenchmarkAttributesFromResult measures Result-to-attributes
// conversion for a representative Result — a mature, familiar
// fingerprint scored as ordinary traffic, the common case a future
// Collector processor would enrich most spans against. Building the
// Result (via a real Engine.Analyze call) runs once, outside the timed
// loop; only AttributesFromResult itself is measured, mirroring
// BenchmarkEventFromSpan's split in this same file.
func BenchmarkAttributesFromResult(b *testing.B) {
	engine := trustvian.NewEngine()
	ev := event.Event{
		ID:        "evt-bench",
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Actor: event.Actor{
			ID:                 "svc-payment",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "POST /payment"},
		Target:    event.Target{Name: "payment-db"},
		Context:   event.Context{Environment: "production"},
	}

	result, err := engine.Analyze(context.Background(), ev)
	if err != nil {
		b.Fatalf("Analyze() error = %v", err)
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = trustvianotel.AttributesFromResult(result)
	}
}
