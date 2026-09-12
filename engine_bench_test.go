package trustvian_test

import (
	"context"
	"fmt"
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

// BenchmarkEngineAnalyzeTransitionDeviation measures the same
// steady-state pipeline as BenchmarkEngineAnalyze, but with task 025's
// transition_deviation signal enabled (TransitionWeight > 0) —
// completing the per-signal benchmark matrix task 029's own
// stabilization pass asked for (previously only transition_rarity,
// n-gram, and Markov had a dedicated engine-level benchmark; this
// signal itself was only ever measured at the internal/anomaly level,
// via BenchmarkScoreTransitionDeviation). Compare against
// BenchmarkEngineAnalyze (TransitionWeight defaults to 0) to see this
// signal's own added cost through the full pipeline.
func BenchmarkEngineAnalyzeTransitionDeviation(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7
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

// BenchmarkEngineAnalyzeMarkov measures the same steady-state pipeline
// as BenchmarkEngineAnalyze, but with task 028's markov_surprisal
// signal enabled (MarkovWeight > 0). warmUpEngine's 30 repeated
// self-transitions clear MinTransitionObservations (20) well before
// the steady-state calls begin, so markov_surprisal computes its full
// arithmetic on every call rather than short-circuiting on the
// cold-start gate — mirroring BenchmarkEngineAnalyzeTransitionRarity's
// own reuse of warmUpEngine one level over. Compare against
// BenchmarkEngineAnalyze (MarkovWeight defaults to 0, i.e. the task
// 027 baseline) to see this signal's own added cost through the full
// pipeline.
func BenchmarkEngineAnalyzeMarkov(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.MarkovWeight = 0.7
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

// BenchmarkEngineAnalyzeFullBehavioral measures the same steady-state
// pipeline with every v0.6 behavioral signal weight enabled at once —
// the realistic "operator turned everything on" worst case, and the
// scenario that most matters for confirming the
// transition_rarity/markov_surprisal mutual-exclusion rule (ADR 0013)
// doesn't itself add meaningful cost on the hot path: both weights are
// set here, but only one ever contributes to combine() per call (see
// anomaly.Score's own MarkovWeight handling). Compare against
// BenchmarkEngineAnalyze (every weight at its 0 default) for the full,
// cumulative cost of enabling all of tasks 025-028 together.
func BenchmarkEngineAnalyzeFullBehavioral(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7
	cfg.TransitionRarityWeight = 0.7
	cfg.NGramWeight = 0.7
	cfg.NGramRarityWeight = 0.7
	cfg.MarkovWeight = 0.7
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

// BenchmarkEngineAnalyzeDelegationAbsent measures the same steady-state
// pipeline as BenchmarkEngineAnalyze, but with task 031's
// delegation_deviation signal enabled (DelegationWeight > 0) against
// events that never carry Context.DelegatedFrom — the common,
// non-agent case. Compare against BenchmarkEngineAnalyze to confirm
// enabling the weight costs nothing when no event ever exercises the
// signal: delegationSignal is gated on feat.Volatile.DelegatedFrom !=
// "" before it is even called (see anomaly.Score), so this measures
// only that gate check's own (expected-zero) cost.
func BenchmarkEngineAnalyzeDelegationAbsent(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.DelegationWeight = 0.7
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

// warmUpEngineWithDelegator mirrors warmUpEngine's exact shape and
// rationale (a fixed, one-second cadence, not time.Now(), to avoid a
// spurious frequency_deviation reading), but for AI-agent tool-call
// events that consistently carry the same delegator — the fixture
// task 031's own familiar/novel delegation benchmarks both build on.
func warmUpEngineWithDelegator(b *testing.B, engine *trustvian.Engine, ctx context.Context, delegator string) time.Time {
	b.Helper()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 30 {
		clock = clock.Add(time.Second)
		ev := agentEvent("agent-b", fmt.Sprintf("session-%d", i), "search", clock)
		ev.Context.DelegatedFrom = delegator
		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			b.Fatal(err)
		}
	}
	return clock
}

// BenchmarkEngineAnalyzeDelegationFamiliar measures the same
// steady-state pipeline with delegation_deviation enabled, against a
// delegator this actor has seen 30 times already — the common,
// "nothing is wrong" case for a genuinely agentic workload.
func BenchmarkEngineAnalyzeDelegationFamiliar(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.DelegationWeight = 0.7
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(cfg))
	ctx := context.Background()

	lastWarmUp := warmUpEngineWithDelegator(b, engine, ctx, "agent-a")
	steadyStateTS := lastWarmUp.Add(time.Second)

	b.ReportAllocs()
	for b.Loop() {
		ev := agentEvent("agent-b", "session-steady-state", "search", steadyStateTS)
		ev.Context.DelegatedFrom = "agent-a"
		if _, err := engine.Analyze(ctx, ev); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEngineAnalyzeDelegationNovel measures the same steady-state
// pipeline with delegation_deviation enabled, against a delegator this
// actor has never seen — the worst case, where delegationSignal's
// Detail string is actually formatted on every call.
func BenchmarkEngineAnalyzeDelegationNovel(b *testing.B) {
	cfg := anomaly.DefaultConfig()
	cfg.DelegationWeight = 0.7
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(cfg))
	ctx := context.Background()

	lastWarmUp := warmUpEngineWithDelegator(b, engine, ctx, "agent-a")
	steadyStateTS := lastWarmUp.Add(time.Second)

	b.ReportAllocs()
	for b.Loop() {
		ev := agentEvent("agent-b", "session-steady-state", "search", steadyStateTS)
		ev.Context.DelegatedFrom = "agent-x" // never observed for agent-b
		if _, err := engine.Analyze(ctx, ev); err != nil {
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
