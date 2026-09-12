# 026 — Transition Rarity / Frequency-Based Transition Deviation

**Milestone:** v0.6 · **Depends on:** [025](025-sequence-analysis-foundation.md)
(the transition foundation this task evolves, in place — the same
`Baseline`/`Store` infrastructure, no parallel state) · **Blocks:**
n-gram/Markov detection (illustrative, unscoped —
[docs/ROADMAP.md § v0.6](../ROADMAP.md#v06--behavioral-detection-depth))
· **Independent of:** `v0.5` (`config`/`alert`) — nothing here touches
Policy or Alert configuration.

## Objective

Evolve task 025's binary `transition_deviation` (seen vs. never seen)
into a graded measure of how *common* a seen transition is —
common/uncommon/rare, not just seen/unseen — without building a Markov
model, n-gram, or ML component. See
[ADR 0011](../adr/0011-transition-rarity-statistic-and-orientation.md)
for the full statistical definition and orientation proof, and
[docs/sequence-analysis.md § Transition rarity](../sequence-analysis.md#transition-rarity-v06-task-026)
for the design writeup.

## Why

`docs/ROADMAP.md` § v0.6 named this precisely, under "Not yet done":
*"task 025's `transition_deviation` is binary (seen vs. never seen); a
future slice could add a frequency-based 'rare, not merely unseen'
threshold, still without a probability model."* A `read -> export`
transition that has happened twice out of two thousand outgoing
transitions is meaningfully different from one that happens 40% of the
time, even though both are "seen" — `transition_deviation` alone cannot
distinguish them.

## Statistical definition (the central correctness question)

`FingerprintStats.PredecessorCounts` (task 025) lives on the
*destination* fingerprint and answers "of the times destination B was
reached, how often did predecessor A lead here" —
`P(predecessor | destination)`. The question a transition-rarity
detector actually needs is the reverse: "given what this actor just
did (A), how unusual is doing B next" — `P(destination | predecessor)`.
These are different statistics, and reading `PredecessorCounts` alone
as if it answered the second would be silently wrong. See
[ADR 0011 § The orientation problem](../adr/0011-transition-rarity-statistic-and-orientation.md#the-orientation-problem)
for the full proof.

**Resolution:** one new scalar, `FingerprintStats.OutgoingTransitionTotal
uint64` — the count of valid transitions where *this* fingerprint was
the predecessor, to any destination — living on the predecessor's own
stats:

```
frequency(A -> B) = PredecessorCounts_B[A] / OutgoingTransitionTotal_A
rarity(A -> B)     = 1 - frequency(A -> B)      (only once minimum support is met)
```

Both terms are O(1) map/field reads; no scan of `Baseline.Fingerprints`
is required. This is a genuine empirical relative frequency (a
maximum-likelihood estimate of `P(B|A)`) — deliberately called
*frequency*/*rarity*, never *probability* or *Markov transition
probability*: no smoothing, no stationary distribution, no chain. See
ADR 0011 for why.

## Scope

```text
Baseline.Observe (write path)
      ↓
recordPredecessor advances PredecessorCounts_B[A]   (task 025, unchanged)
      ↓
observeOutgoingTransition advances OutgoingTransitionTotal_A   (new, this task)

anomaly.Score (read path)
      ↓
transitionSignal   -> "transition_deviation"   (task 025, unchanged: count == 0)
transitionRaritySignal -> "transition_rarity"  (new, this task: count > 0, gated)
      ↓
Anomaly.Contributors / Anomaly.Score  (existing Result shape, unchanged)
```

- **`internal/baseline`**: `FingerprintStats` gains
  `OutgoingTransitionTotal uint64`; `Baseline.Observe` increments it on
  the predecessor's own map entry, under the identical
  forward-timestamp-only ordering guard the rest of the transition
  state already uses. Plain, unguarded `++` — no saturation logic (see
  Non-Goals).
- **`internal/anomaly`**: `Config` gains `MinTransitionObservations
  uint64` (default `20`) and `TransitionRarityWeight float64` (default
  `0`, opt-in — identical precedent to `TransitionWeight` before it);
  `Score` gains one new signal, `transition_rarity`, via the new
  `transitionRaritySignal` helper, called alongside (not instead of)
  `transitionSignal`.
- **Zero changes** to `engine.go`, `options.go`, `internal/store`,
  `internal/trust`, `internal/policy`, `alert`, `config`,
  `cmd/trustvian`, `processor/` — identical reuse pattern to task 025.

## Non-Goals

- **No Markov model.** `frequency(A->B)` is mathematically a
  first-order Markov transition-probability estimate, but this task
  does not build a transition matrix, a stationary distribution,
  smoothing, or a `P(*|A)` full row — see
  [ADR 0011 § Why Markov still waits](../adr/0011-transition-rarity-statistic-and-orientation.md#why-markov-still-waits).
- **No n-gram / higher-order history.** `A` remains a single
  predecessor (task 025's own one-step scope), not a state derived from
  the last N actions.
- **No fake "probability" naming.** The signal and its `Detail` string
  say "rarity"/"frequency", never "probability".
- **No nonlinear frequency-to-severity transform.** `rarity = 1 -
  frequency` is linear, applied only after the minimum-support gate —
  see [ADR 0011 § Mapping frequency to a bounded signal value](../adr/0011-transition-rarity-statistic-and-orientation.md#mapping-frequency-to-a-bounded-signal-value)
  for why a sqrt/log/threshold-band transform was considered and
  rejected.
- **No saturating-counter logic.** `OutgoingTransitionTotal++` matches
  `FingerprintStats.Count`'s own existing unguarded increment —
  introducing saturation for only this one field would be an
  inconsistent special case, not a genuine safety improvement.
- **No new cardinality dimension.** `OutgoingTransitionTotal` is one
  scalar per existing `FingerprintStats` entry, not a new map — the
  pre-existing `maxPredecessors = 64` bound (task 025/ADR 0010) is
  unaffected and remains sufficient.
- **No collapsing of unseen and rare into one signal.**
  `transition_deviation` and `transition_rarity` stay mutually
  exclusive by construction (`count == 0` vs. `count > 0`) — see ADR
  0011 § Preserving the unseen/rare distinction.
- **No ML, ML-adjacent library, or external anomaly service.** Purely
  deterministic Go standard library, identical to task 025.
- **No new public API, CLI, Collector, or config-schema changes.**
  Activation is `trustvian.WithAnomalyConfig(anomaly.Config{TransitionRarityWeight: ...})`,
  the same existing option task 025 already used.

## Technical Requirements

- Every pre-existing test (including all of task 025's) passes
  completely unmodified — proof this task does not change any
  `v0.5`/task-025-era `Score`/`Decision` output for a caller that
  hasn't set `TransitionRarityWeight`.
- `transitionRaritySignal` only computes its ratio once
  `OutgoingTransitionTotal_A >= Config.MinTransitionObservations`
  (default `20`); below that, the signal does not fire at all — not
  "fires weakly" — identical cold-start philosophy to
  `frequency_deviation`'s own `IntervalObservations == 0` gate.
- `transition_deviation` and `transition_rarity` are mutually exclusive
  by construction for the same transition, not merely by convention.
- `rarity` is always in `[0, 1]` by construction (a count divided by a
  total that includes it) — no `NaN`/`Inf` possible once the gate has
  passed.
- Learning-vs-detection ordering is preserved: `anomaly.Score` (called
  only from read-only `Engine.Analyze`) never mutates `Baseline`;
  `Baseline.Observe`'s counter advance is only reachable via
  `Engine.Observe`. Inherited from the pre-existing architectural
  separation, not new code — proven for this signal specifically by
  `TestAnalyzeTransitionRarityScoresBeforeLearning`.
- Baseline-poisoning resistance is inherited for free from the
  pre-existing `eligibleForLearning` gate in `engine.go` (`Engine.Observe`
  only learns from `ALLOW`/`OBSERVE_ONLY`/`ALERT` decisions) — proven
  for the new counters specifically by
  `TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions`.
- Actor/environment isolation is preserved: `OutgoingTransitionTotal`
  lives on the same `baseline.Key`-scoped `FingerprintStats` every other
  learned field already uses — proven by
  `TestAnalyzeTransitionRarityCrossActorIsolation`.
- `TransitionRarityWeight` defaults to `0` — `DefaultConfig()` and any
  existing caller-constructed `Config` literal that doesn't set it get
  byte-for-byte unchanged `Score` output.

## Tests

`internal/baseline/baseline_test.go` (+5):
`TestBaselineObserveTracksOutgoingTransitionTotal`,
`TestBaselineObserveOutgoingTransitionTotalSelfTransition`,
`TestBaselineObserveOutOfOrderEventDoesNotIncrementOutgoingTotal`,
`TestBaselineObserveOutgoingTransitionTotalIsImmutable`,
`TestBaselineObserveManyDistinctTransitionsStayBounded` (5,000 distinct
predecessors to one shared destination — proves `PredecessorCounts`
stays capped at 64 while `OutgoingTransitionTotal` is correctly `1` for
every one of the 5,000 distinct predecessors; scale matches this
codebase's existing precedent for this kind of test,
`TestObserveUnboundedFingerprintsDoesNotPanic`, rather than the task
brief's own illustrative "10,000," which would have made this single
test disproportionately slow for the same proof).

`internal/anomaly/anomaly_test.go` (+4):
`TestDefaultConfigTransitionRarityWeightIsOptIn`,
`TestScoreTransitionRarityOrdering` (a 900/90/10-split worked example;
asserts strict `common < uncommon < rare` ordering and that unseen vs.
seen-and-rare remain mutually exclusive),
`TestScoreTransitionRarityColdStart` (table: 0, 1, `MinTransitionObservations
- 1`, `MinTransitionObservations`, `MinTransitionObservations + 1`
outgoing observations),
`TestScoreMatchesDocumentedFormulaForTransitionRarity` (exact
arithmetic against the documented noisy-OR combination, matching this
codebase's own bar for a security-relevant number),
`TestScoreTransitionRarityNeverExceedsBounds` (property test across
multiple totals: never `NaN`/`Inf`, always in `[0, 1]`).

`engine_test.go` (+4):
`TestAnalyzeTransitionRarityEndToEnd` (the task's own central
integration proof, built through the real, gated `Analyze`+`Observe`
loop, per `.claude/rules/testing.md`'s "end-to-end tests are
load-bearing" convention — a rare destination scores a strictly higher
rarity than a common one, and the rare case's `Trust.Risk` is at least
as high; exact accumulated counts are policy-dependent, so this test
asserts the qualitative ordering property, leaving exact-arithmetic
verification to the dedicated unit test above),
`TestAnalyzeTransitionRarityCrossActorIsolation`,
`TestAnalyzeTransitionRarityScoresBeforeLearning` (mirrors the
pre-existing `TestAnalyzeIsReadOnly`),
`TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions` (the
poisoning-guard regression: repeating a `BLOCK`ed transition never
grows `OutgoingTransitionTotal` or `PredecessorCounts`).

All of the above run under `go test ./... -race -count=1`.

## Benchmarks

`internal/anomaly/anomaly_bench_test.go` (+1): `BenchmarkScoreTransitionRarity`
— a predecessor with 52 total outgoing transitions, only 2 of them to
the destination under test (a rare-but-seen, past-minimum-support
transition, so the signal actually fires and its `Detail` string is
built — the worst case, mirroring `BenchmarkScoreTransitionDeviation`'s
own choice).

`engine_bench_test.go` (+1): `BenchmarkEngineAnalyzeTransitionRarity` —
the same steady-state pipeline as `BenchmarkEngineAnalyze`, with
`TransitionRarityWeight` set and enough warm-up observations to clear
`MinTransitionObservations`.

Measured (Apple M3 Pro; see [PERFORMANCE.md § v0.6 task
026](../PERFORMANCE.md#v06-task-026-transition-rarity) for the full
table and discussion, including a same-machine A/B against task 025 in
a disposable worktree):

| Benchmark | Before (task 025) | After (task 026) |
|---|---:|---:|
| `BenchmarkScoreKnownFamiliar` | 98.77 ns/op, 0 B/op, 0 allocs | 136.3–136.9 ns/op, 0 B/op, 0 allocs |
| `BenchmarkScoreTransitionRarity` (new) | — | 364.7–373.1 ns/op, 296 B/op, 5 allocs |
| `BenchmarkObserveTransition` | 376.3 ns/op, 1,344 B/op, 6 allocs | 420.0–420.8 ns/op, 1,408 B/op, 6 allocs |
| `BenchmarkEngineAnalyze` | 467.9–471.8 ns/op, 456 B/op, 17 allocs | 506.4–512.5 ns/op, 456 B/op, 17 allocs |

**`Engine.Analyze`'s allocation profile stays byte-for-byte unchanged**
(`456 B/op, 17 allocs/op`), but its latency genuinely grows by
~35–40ns/call — `transitionRaritySignal` runs unconditionally alongside
`transitionSignal` whenever a predecessor exists, the same
"always-compute, weight-gate the contribution" pattern every prior
signal already follows, so this small cost is paid regardless of
whether `TransitionRarityWeight` is ever set above its `0` default.
Reported, not hidden — see PERFORMANCE.md for the full accounting.

## Documentation

- [docs/adr/0011-transition-rarity-statistic-and-orientation.md](../adr/0011-transition-rarity-statistic-and-orientation.md)
  (new): the statistical-definition ADR.
- [docs/sequence-analysis.md](../sequence-analysis.md): new "Transition
  rarity" section.
- [docs/DOMAIN.md](../DOMAIN.md): "Sequence-aware detection" section
  extended to cover `transition_rarity`.
- [docs/SECURITY.md](../SECURITY.md): "Sequence state" section extended
  with the counter-overflow/cold-start/poisoning considerations specific
  to `OutgoingTransitionTotal`.
- [docs/PERFORMANCE.md](../PERFORMANCE.md): new benchmark results.
- [docs/ROADMAP.md](../ROADMAP.md): task 026 marked done under `v0.6`;
  n-gram behavioral detection named as the next illustrative, unscoped
  slice.
- [trustvian-project-spec.md](../../trustvian-project-spec.md) § 6:
  status callout extended to distinguish transition rarity from
  transition novelty.
- [README.md](../../README.md): `v0.6` progress line updated.
- [CHANGELOG.md](../../CHANGELOG.md): new `## Unreleased` → `### Added`
  entry.

## Acceptance Criteria

- `go test ./... -race -count=1` green at the repository root,
  including every new test above, and every pre-existing test
  (including all of task 025's) unmodified and still passing.
- `go test -bench=. -benchmem ./...` runs and reports the numbers in
  the table above; `BenchmarkEngineAnalyze`'s allocation profile
  (`B/op`, `allocs/op`) stays byte-for-byte unchanged from task 025.
- `gofmt -l .`, `go vet ./...` clean.
- No new dependency (`git diff go.mod go.sum` empty).
- `processor/go.mod` unaffected (this task touches no public API
  `processor/` could depend on) — verified with `GOWORK=off go build
  ./... && GOWORK=off go test ./... -race` inside `processor/`.
- No `SequenceStore`/`SequenceKey`/`SequenceAnalyzer`/probability-named
  type exists anywhere, public or internal.
- `TestAnalyzeTransitionRarityEndToEnd` proves the full `Event ->
  Engine -> transition_rarity signal -> Result` path through the real,
  gated learning loop — not a synthetic, directly-seeded baseline.
