package trustvian_test

import (
	"context"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/internal/anomaly"
)

// warmUpEngine builds a mature, familiar baseline via 30 Analyze+Observe
// calls spaced exactly one second apart on a fixed clock (not time.Now()
// in a tight loop): the frequency_deviation signal (task 004) treats the
// wall-clock gap between calls as the actor's request rate, and a tight
// benchmark loop's microsecond-scale, jittery gaps would make every
// "steady-state" call look like a rate spike relative to that gap, rather
// than the intended zero-signal common case. It returns the timestamp of
// the last warm-up call, so the caller can derive a steady-state event's
// Timestamp that continues the same one-second cadence.
func warmUpEngine(b *testing.B, engine *trustvian.Engine, ctx context.Context) time.Time {
	b.Helper()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 30 {
		clock = clock.Add(time.Second)
		result, err := engine.Analyze(ctx, paymentEventAt(10, "warm-up", clock))
		if err != nil {
			b.Fatal(err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			b.Fatal(err)
		}
	}
	return clock
}

// BenchmarkEngineAnalyze measures the full Event -> ... -> Decision
// pipeline for the common case: a mature, familiar fingerprint matching
// its baseline exactly.
func BenchmarkEngineAnalyze(b *testing.B) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	lastWarmUp := warmUpEngine(b, engine, ctx)
	steadyStateTS := lastWarmUp.Add(time.Second)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := engine.Analyze(ctx, paymentEventAt(10, "steady-state", steadyStateTS)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEngineAnalyzeTransitionRarity measures the same steady-state
// pipeline as BenchmarkEngineAnalyze, but with task 026's transition_rarity
// signal enabled (TransitionRarityWeight > 0) and enough warm-up
// observations for the predecessor to clear MinTransitionObservations, so
// transitionRaritySignal actually computes on every call rather than
// short-circuiting on the cold-start gate. Compare against
// BenchmarkEngineAnalyze (task 026's signal weight defaults to 0, i.e. the
// task 025 transition-foundation-only baseline) to see this signal's own
// added cost through the full pipeline.
func BenchmarkEngineAnalyzeTransitionRarity(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.TransitionRarityWeight = 0.7
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(cfg))
	ctx := context.Background()

	lastWarmUp := warmUpEngine(b, engine, ctx)
	steadyStateTS := lastWarmUp.Add(time.Second)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := engine.Analyze(ctx, paymentEventAt(10, "steady-state", steadyStateTS)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEngineAnalyzeNGram measures the same steady-state pipeline
// as BenchmarkEngineAnalyze, but with task 027's ngram_deviation/
// ngram_rarity signals enabled (NGramWeight, NGramRarityWeight > 0).
// warmUpEngine's 30 repeated self-transitions also form a familiar
// self-referential 3-gram after the first two, so both new signals
// compute on every steady-state call rather than short-circuiting on
// the cold-start gate — mirroring
// BenchmarkEngineAnalyzeTransitionRarity's own reuse of warmUpEngine
// one level up. Compare against BenchmarkEngineAnalyze (both weights
// default to 0, i.e. the task 026 baseline) to see these signals' own
// added cost through the full pipeline.
func BenchmarkEngineAnalyzeNGram(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.NGramWeight = 0.7
	cfg.NGramRarityWeight = 0.7
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(cfg))
	ctx := context.Background()

	lastWarmUp := warmUpEngine(b, engine, ctx)
	steadyStateTS := lastWarmUp.Add(time.Second)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := engine.Analyze(ctx, paymentEventAt(10, "steady-state", steadyStateTS)); err != nil {
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

	lastWarmUp := warmUpEngine(b, engine, ctx)
	steadyStateTS := lastWarmUp.Add(time.Second)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := engine.Analyze(ctx, paymentEventAt(10, "steady-state", steadyStateTS)); err != nil {
				b.Fatal(err)
			}
		}
	})
}
