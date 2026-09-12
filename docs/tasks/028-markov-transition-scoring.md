# 028 — Markov Transition Scoring (First-Order Surprisal)

**Milestone:** v0.6 · **Depends on:** [025](025-sequence-analysis-foundation.md)
(pairwise state), [026](026-transition-rarity.md) (the exact
numerator/denominator this task reuses in full) · **Independent of:**
[027](027-bounded-ngram-detection.md) — orthogonal, not merged (see
Non-Goals) · **Blocks:** Sequence Config Integration (illustrative,
unscoped — [docs/ROADMAP.md § v0.6](../ROADMAP.md#v06--behavioral-detection-depth)).

## Objective

Add a first-order Markov transition signal — **only if it provides
capability `transition_rarity` (task 026) does not already provide**.
This task's mandatory first deliverable, done before any code, was
answering that question. See
[ADR 0013](../adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md)
for the full analysis and its conclusion.

## The mandatory duplication finding

`transition_rarity = 1 - P(B|A)` and the textbook Markov statistic
`surprisal = -log(P(B|A))` are both strictly monotonic functions of the
**identical** frequency, `count(A→B) / OutgoingTransitionTotal_A`. They
can never disagree on which of two transitions is rarer — proven
directly by `TestMarkovSurprisalIsMonotonicReparameterizationOfRarity`.
**Implementing raw Markov scoring here would duplicate task 026's
evidence, not add to it.**

What genuinely differs is *curve shape*, not evidence: linear
`1-frequency` saturates near `1` quickly across the rare tail;
`-log2(frequency)`, before bounding, keeps growing without bound —
retaining resolution deep into "extraordinarily rare" that the linear
mapping loses. This is a legitimate reason to offer an alternative
severity curve over the same evidence; it is not a reason to score
both curves independently and additively.

## Scope

```text
Baseline (task 025/026 state, unchanged, no new fields)
      ↓
PredecessorCounts / OutgoingTransitionTotal (reused, not extended)

anomaly.Score (read path)
      ↓
transitionRaritySignal   -> "transition_rarity"    (existing, task 026)
markovSurprisalSignal    -> "markov_surprisal"      (new, task 028)
      ↓
Score(): if MarkovWeight > 0, transition_rarity's own contribution to
combine() is forced to zero — enforced in code, not by convention
      ↓
Anomaly.Contributors / Anomaly.Score  (existing Result shape, unchanged)
```

- **`internal/baseline`**: **no changes.** Markov reuses
  `FingerprintStats.PredecessorCounts`/`OutgoingTransitionTotal` — the
  identical state `transitionRaritySignal` already reads. No new
  scalar, no new map, no new bound.
- **`internal/anomaly`**: `Config` gains exactly one new field,
  `MarkovWeight float64` (defaults to `0`, opt-in — the exact
  precedent every prior v0.6 signal weight already set); `Score` gains
  one new signal, `markov_surprisal`, via the new
  `markovSurprisalSignal` helper, plus a small inline rule at the
  transition-signal call site: when `cfg.MarkovWeight > 0`,
  `transition_rarity`'s `Signal.Weight` is forced to `0` before being
  appended to `Contributors` (still visible for explainability; no
  longer scored).
- **Zero changes** to `engine.go`, `internal/store`, `internal/policy`,
  `internal/trust`, `alert`, `config`, the CLI, or `processor/` —
  identical reuse discipline to tasks 025–027.

## Non-Goals

- **No independent double-weighting of the same evidence.**
  `transition_rarity` and `markov_surprisal` never both contribute to
  `Score` at once — see "Double-counting decision" in ADR 0013.
- **No higher-order Markov, HMM, stationary distribution, Viterbi,
  Monte Carlo, or Bayesian inference.** Strictly `state(t-1) ->
  state(t)`, matching task 025/026's own scope.
- **No merging with task 027's bounded n-grams.** `ngram_deviation`/
  `ngram_rarity` answer a different question (a specific two-fingerprint
  predecessor *pair*), using different state (`TrigramCounts`/
  `TrigramContinuationTotal`) this task does not touch. Both can
  legitimately co-fire on the same event; `combine()`'s existing
  clamped noisy-OR keeps that bounded, unchanged by this task.
- **No smoothing (Laplace or otherwise), no epsilon floor.** Unseen
  transitions remain entirely `transition_deviation`'s domain (task
  025); `markovSurprisalSignal` never evaluates `count == 0`, so
  `-log2(0)` is never computed — Inf/NaN are impossible by
  construction, not by a defensive clamp.
- **No separate Markov-specific minimum-support field.**
  `MinTransitionObservations` (task 026) is reused exactly, since both
  signals share the identical denominator.
- **No new public API, CLI, Collector, or config-schema changes.**
  Activation is `trustvian.WithAnomalyConfig(anomaly.Config{MarkovWeight: ...})`,
  the same existing option every prior v0.6 task used.
- **No third-party math/statistics dependency.** `math.Log2` (Go
  stdlib) is sufficient.

## Technical Requirements

- Every pre-existing test (tasks 001–027) passes completely
  unmodified — proof this task does not change any prior `Score`/
  `Decision` output for a caller that hasn't set `MarkovWeight`.
- `markovSurprisalSignal` shares `transitionRaritySignal`'s exact
  gates: `count == 0` never reaches this signal (transition_deviation's
  domain); below `MinTransitionObservations`, no signal fires at all.
- `markov_surprisal`'s `Value` is always in `[0,1)` — never negative,
  never `>= 1`, never `NaN`, never `Inf` — proven by
  `TestScoreMarkovSurprisalNeverExceedsBounds` across a range of totals
  up to 10,000.
- Strict monotonicity: `P(B|A) > P(C|A)` implies
  `surprisal(A→B) < surprisal(A→C)` — proven by
  `TestScoreMarkovSurprisalOrdering` (a 900/90/10 worked example).
- `MarkovWeight` defaults to `0` — `DefaultConfig()` and any existing
  caller-constructed `Config` literal that doesn't set it get
  byte-for-byte unchanged `Score` output.
- Setting both `TransitionRarityWeight` and `MarkovWeight` to nonzero
  values produces a `Score` identical to setting `MarkovWeight` alone —
  proven by `TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring`.

## Tests

`internal/anomaly/anomaly_test.go` (+10):
`TestDefaultConfigMarkovWeightIsOptIn`,
`TestScoreMarkovSurprisalKnownNumeratorDenominator` (exact
surprisal/normalization arithmetic),
`TestScoreMarkovSurprisalOrdering` (900/90/10 worked example, strict
`common < uncommon < rare` surprisal ordering),
`TestScoreMarkovSurprisalColdStart` (minimum-support table),
`TestScoreMarkovSurprisalUnseenTransitionNeverFires` (zero-probability
policy proof — `transition_deviation` fires, `markov_surprisal` never
does, `Score` stays finite),
`TestScoreMarkovSurprisalNeverExceedsBounds` (property test),
**`TestMarkovSurprisalIsMonotonicReparameterizationOfRarity`** — the
task's own mandatory Critical Duplication Test: proves
`transition_rarity` and `markov_surprisal`, scored independently, never
disagree on relative ordering across six distinct transitions,
**`TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring`** —
the mandatory combined-score/double-counting proof: `Score` with both
weights set equals `Score` with only `MarkovWeight` set,
`TestScoreCombinedMarkovAndNGramSignalsRemainBounded` (Markov +
n-gram co-firing stays finite and in `[0,1]`).

`engine_test.go` (+3): `TestAnalyzeMarkovCrossActorIsolation`,
`TestAnalyzeMarkovScoresBeforeLearning` (mirrors
`TestAnalyzeTransitionRarityScoresBeforeLearning`),
`TestObserveMarkovLearnsOnlyFromEligibleDecisions` (the poisoning-guard
regression).

**Regression proof (§38 of the task brief):** the task-027 critical
scenario — `TestScoreNGramDeviationDetectsNovelTrigramDespiteFamiliarPairwiseTransitions`
— was re-run explicitly and passes unmodified; this task touches no
n-gram code.

**No new concurrency test.** Markov introduces no new mutable state —
it reads the identical `PredecessorCounts`/`OutgoingTransitionTotal`
fields task 026's own `TestInMemoryObserveConcurrentTransitionTracking`
already stresses under `-race`. Concurrency safety is inherited, not
re-proven from scratch, confirmed by the full `go test ./... -race
-count=1` sweep.

All of the above run under `go test ./... -race -count=1`.

## Benchmarks

`internal/anomaly/anomaly_bench_test.go` (+2): `BenchmarkScoreMarkovSurprisal`
(the worst case where the signal fires, mirroring
`BenchmarkScoreTransitionRarity`'s exact shape), `BenchmarkScoreMarkovLookup`
(the common, non-firing, below-minimum-support case — the O(1) "lookup"
cost the task brief asks to isolate).

`engine_bench_test.go` (+1): `BenchmarkEngineAnalyzeMarkov` — the same
steady-state pipeline as `BenchmarkEngineAnalyze`, with `MarkovWeight`
enabled and enough warm-up observations to clear
`MinTransitionObservations`.

Measured (Apple M3 Pro; see [PERFORMANCE.md § v0.6 task
028](../PERFORMANCE.md#v06-task-028-markov-transition-scoring) for the
full table, a same-machine A/B against task 027 in a disposable
worktree, and full discussion of a genuine, unavoidable interaction
with `BenchmarkScoreTransitionRarity`'s own existing fixture):

| Benchmark | Before (task 027) | After (task 028) |
|---|---:|---:|
| `BenchmarkScoreKnownFamiliar` | 205.7–210.7 ns/op, 0 B/op, 0 allocs | 255.0–260.5 ns/op, 0 B/op, 0 allocs |
| `BenchmarkScoreTransitionRarity` | 434.2–437.3 ns/op, 296 B/op, 5 allocs | 722.0–745.4 ns/op, 648 B/op, 9 allocs (see below) |
| `BenchmarkScoreMarkovSurprisal` (new) | — | 722.0–728.9 ns/op, 648 B/op, 9 allocs |
| `BenchmarkScoreMarkovLookup` (new) | — | 280.7–285.7 ns/op, 112 B/op, 2 allocs |
| `BenchmarkEngineAnalyze` | 597.2–606.5 ns/op, 456 B/op, 17 allocs | 645.8–652.3 ns/op, 456 B/op, 17 allocs |
| `BenchmarkEngineAnalyzeMarkov` (new) | — | 650.2–652.3 ns/op, 456 B/op, 17 allocs |

**`Engine.Analyze`'s allocation profile stays byte-for-byte unchanged**
(`456 B/op, 17 allocs/op`), but latency grows by ~45–50ns/call —
`markovSurprisalSignal` runs unconditionally alongside the existing
transition signals whenever a predecessor exists, the "always compute,
weight-gate the contribution" pattern every prior signal already
follows.

**`BenchmarkScoreTransitionRarity`'s allocation profile genuinely
changed (296 B/5 allocs → 648 B/9 allocs), and this was deliberately
NOT corrected in the fixture** — unlike task 027's own
`BenchmarkScoreTransitionDeviation` fix, this is not an accidental
fixture leak: `markov_surprisal` shares `transition_rarity`'s exact
gate by design (ADR 0013), so any fixture with enough history to fire
one always fires the other too, regardless of `MarkovWeight`'s value
(Score's append condition is `Value > 0`, weight-independent). There is
no way to isolate `transitionRaritySignal`'s cost alone anymore without
breaking the minimum-support gate it itself needs to fire. See the
benchmark's own updated doc comment in
`internal/anomaly/anomaly_bench_test.go`.

## Documentation

- [docs/adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md](../adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md)
  (new): the mandatory duplication analysis and design-decision ADR.
- [docs/DOMAIN.md](../DOMAIN.md): "Sequence-aware detection" section
  extended to cover `markov_surprisal` and its relationship to
  `transition_rarity`.
- [docs/SECURITY.md](../SECURITY.md): "Sequence state" section
  extended with the zero-probability/correlated-signal-amplification
  considerations specific to this signal.
- [docs/PERFORMANCE.md](../PERFORMANCE.md): new benchmark results.
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md): confirmed no change
  needed (no new package, no new pipeline stage, no dependency-direction
  change) — verified explicitly.
- [docs/ROADMAP.md](../ROADMAP.md): task 028 marked done under `v0.6`;
  Sequence Config Integration named as the next illustrative slice.
- [trustvian-project-spec.md](../../trustvian-project-spec.md) § 6:
  status callout extended.
- [README.md](../../README.md): `v0.6` progress line updated.
- [CHANGELOG.md](../../CHANGELOG.md): new `## Unreleased` → `### Added`
  entry.

## Acceptance Criteria

- Mathematical definition documented (ADR 0013) — done.
- Distinct value vs. `transition_rarity` established: a different
  severity curve over the same evidence, never claimed as new evidence
  — done, proven by `TestMarkovSurprisalIsMonotonicReparameterizationOfRarity`.
- No double counting: proven by
  `TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring`.
- Bounded score: `[0,1)`, proven by
  `TestScoreMarkovSurprisalNeverExceedsBounds`.
- Cold start: reuses `MinTransitionObservations`, proven by
  `TestScoreMarkovSurprisalColdStart`.
- Zero-probability safety: `Inf`/`NaN` impossible by construction,
  proven by `TestScoreMarkovSurprisalUnseenTransitionNeverFires`.
- Actor isolation: proven by `TestAnalyzeMarkovCrossActorIsolation`.
- Learning eligibility/poisoning resistance: proven by
  `TestObserveMarkovLearnsOnlyFromEligibleDecisions`.
- Ordering: inherited from task 025/026's existing guard (no new
  ordering logic introduced).
- Race safety: `go test ./... -race -count=1` green.
- Benchmarks: reported above, no hidden regression.
- Documentation: listed above, all updated.
- No new dependencies: `git diff go.mod go.sum` empty.
