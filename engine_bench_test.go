package trustvian_test

import (
	"context"
	"testing"

	trustvian "github.com/Trustvian/trustvian"
)

// BenchmarkEngineAnalyze measures the full Event -> ... -> Decision
// pipeline for the common case: a mature, familiar fingerprint matching
// its baseline exactly.
func BenchmarkEngineAnalyze(b *testing.B) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	for range 30 {
		result, err := engine.Analyze(ctx, paymentEvent(10, "warm-up"))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := engine.Analyze(ctx, paymentEvent(10, "steady-state")); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEngineAnalyzeParallel measures the same steady-state pipeline
// under concurrent calls from multiple goroutines, since Analyze is the
// primary hot path and Engine is documented as safe for concurrent use.
func BenchmarkEngineAnalyzeParallel(b *testing.B) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	for range 30 {
		result, err := engine.Analyze(ctx, paymentEvent(10, "warm-up"))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := engine.Analyze(ctx, paymentEvent(10, "steady-state")); err != nil {
				b.Fatal(err)
			}
		}
	})
}
