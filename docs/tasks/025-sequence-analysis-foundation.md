# 025 — Sequence Analysis Foundation

**Milestone:** v0.6 · **Depends on:** `v0.1` (stable `Baseline`/
`Anomaly` — this task extends both, in place) · **Blocks:**
[026](#roadmap-successors-illustrative)–onward (n-gram, Markov, and any
other v0.6 detector build on the transition observation this task
establishes) · **Independent of:** `v0.5` (`config`/`alert`) — nothing
here touches Policy or Alert configuration.

## Objective

Give Trustvian the minimum architecture required to reason about
*order* — `E(n-1) -> E(n)` — rather than only individual events in
isolation, and ship exactly one deterministic detector built on it:
whether the immediately preceding action has ever led to this one
before, for this actor. See
[docs/sequence-analysis.md](../sequence-analysis.md) for the full
design writeup and
[ADR 0010](adr/0010-bounded-process-local-sequence-state.md) for why
the state model looks the way it does.

## Why

`docs/ROADMAP.md` § v0.6 named this precisely: today's anomaly signals
(`categorical_novelty`, `latency_deviation`, `frequency_deviation`,
`error_deviation`, `sensitive_target`, `time_pattern_deviation`) all
score one event against its own fingerprint's history; none of them
see what happened immediately before it. The example this task's own
brief gives is the canonical case: `read -> update` is an actor's
normal path; `read -> delete` may never have happened, even though
`delete` itself, reached some other way, is unremarkable on its own.

## Scope

```text
Baseline.Observe (write path)
      ↓
LastFingerprintID/Time advance (ordering-guarded)
      ↓
FingerprintStats.PredecessorCounts records the transition (bounded)

anomaly.Score (read path)
      ↓
transitionSignal reads PredecessorCounts
      ↓
Signal{Name: "transition_deviation", ...} — noisy-OR combined, opt-in
      ↓
Anomaly.Contributors / Anomaly.Score  (existing Result shape, unchanged)
      ↓
Trust -> Policy -> Decision -> Alert   (all pre-existing, untouched)
```

- **`internal/baseline`**: `Baseline` gains `LastFingerprintID string`
  / `LastFingerprintTime time.Time`; `FingerprintStats` gains
  `PredecessorCounts map[string]uint64` (bounded at `maxPredecessors =
  64`, see ADR 0010); `Baseline.Observe`/`FingerprintStats.observe`
  update them under the same forward-timestamp-only ordering guard the
  existing interval statistics already use.
- **`internal/anomaly`**: `Config` gains `TransitionWeight float64`
  (defaults to `0`, opt-in — identical precedent to
  `FrequencyWeight`/`TimePatternWeight`); `Score` gains one new,
  gated signal block producing `transition_deviation` via the new
  `transitionSignal` helper.
- **Zero changes** to `engine.go`, `options.go`, `internal/store`,
  `internal/trust`, `internal/policy`, `alert`, `config`, `cmd/trustvian`,
  `processor/` — the new signal reaches `Result` through the exact
  existing `Anomaly.Contributors` → `combine()` → `Score` path, and
  activation is purely `trustvian.WithAnomalyConfig(anomaly.Config{TransitionWeight: ...})`,
  the same option that already exists.

## Non-Goals

- **No n-gram or Markov modeling.** `transitionSignal` answers "has
  this exact predecessor ever led here before" (binary: seen/unseen) —
  it does not compute a frequency-based "rare" threshold or a
  probability `P(destination|predecessor)`. Both are named,
  explicitly-deferred future tasks (see docs/ROADMAP.md § v0.6).
- **No sequence longer than one step.** The state model tracks exactly
  one predecessor, not a ring buffer of N prior events — see ADR 0010's
  "Alternatives considered" for why, and what a future n-gram task
  would need to add.
- **No ML, ML-adjacent library, or external anomaly service of any
  kind.** Purely deterministic Go standard library.
- **No new public API.** No `SequenceStore`, `SequenceKey`, or
  `SequenceAnalyzer` type anywhere, public or internal — see ADR 0010.
- **No CLI, Collector, or config-schema changes.** Activation is
  Go-SDK-only, via `WithAnomalyConfig` — no `--sequence` CLI flag, no
  `sequence:` Collector block, no `config` package schema evolution.
  `trustvian analyze` on a single-event file cannot exercise this
  signal meaningfully (there is no predecessor within one invocation
  unless a `Store` is shared across calls, which the CLI does not do
  today) — this is a documented limitation, not a bug, matching
  `baseline build`'s own pre-existing "results don't persist across CLI
  invocations" limitation.
- **No TTL / active eviction / distributed state.** See ADR 0010's
  "Why no TTL" and "Why process-local" sections.

## Technical Requirements

- Every pre-existing `internal/baseline`, `internal/anomaly`, and
  `engine.go`-level test passes completely unmodified — the structural
  proof that this task did not change `v0.5`-era Score/Decision output
  for any caller that hasn't set `TransitionWeight`.
- `Baseline.Observe`'s copy-on-write immutability extends to the new
  fields, including `PredecessorCounts` (a map, hence reference-typed —
  a real bug was caught during this task's own review: an in-place
  `counts[predecessor]++` would have silently corrupted an earlier
  `Baseline` snapshot sharing the same underlying map; fixed via
  `recordPredecessor`'s own copy-on-write, proven by
  `TestBaselineObservePredecessorCountsIsImmutable`).
- An out-of-order/backdated event (a timestamp that does not strictly
  follow `LastFingerprintTime`) contributes no transition information
  in either direction: it does not get scored as following the actual
  last predecessor, and it does not overwrite `LastFingerprintID`/`Time`
  for whatever legitimately follows it — mirroring
  `FingerprintStats.observe`'s existing interval-statistics guard
  exactly, for the identical baseline-poisoning-prevention reason.
- `PredecessorCounts` is bounded at 64 distinct entries per destination
  fingerprint, under concurrent access, without exception — proven by
  `TestInMemoryObserveConcurrentTransitionTracking`.
- `TransitionWeight` defaults to `0` — `DefaultConfig()` and any
  existing caller-constructed `Config` literal that doesn't set it get
  byte-for-byte unchanged `Score` output.

## Tests

`internal/baseline/baseline_test.go` (+7):
`TestBaselineObserveFirstEventHasNoPredecessor`,
`TestBaselineObserveRecordsTransitionBetweenDistinctFingerprints`,
`TestBaselineObserveRecordsRepeatedSelfTransition`,
`TestBaselineObserveOutOfOrderEventDoesNotRecordOrCorruptTransition`,
`TestBaselineObservePredecessorCountsIsImmutable`,
`TestBaselineObservePredecessorCountsIsBounded`.

`internal/anomaly/anomaly_test.go` (+3):
`TestDefaultConfigTransitionWeightIsOptIn`, `TestScoreTransitionDeviation`
(table: familiar/unseen/no-predecessor/out-of-order),
`TestScoreMatchesDocumentedNoisyOrFormulaWithTransitionSignal` (exact
arithmetic, matching this codebase's own bar for a security-relevant
number — not just "the score went up").

`internal/store/store_test.go` (+1):
`TestInMemoryObserveConcurrentTransitionTracking` — many goroutines,
each with its own distinct predecessor fingerprint, racing to observe
(predecessor, then a shared destination) for the same `Key`; asserts
the bound holds and `sum(PredecessorCounts) <= dest.Count` under real
concurrent interleaving (deliberately does *not* assert exact
per-predecessor counts, which are genuinely nondeterministic under
real wall-clock interleaving — see the test's own comment).

`engine_test.go` (+1): `TestAnalyzeTransitionDeviationEndToEnd` — the
task's own central integration proof, built entirely through the real,
gated `Analyze`+`Observe` loop (per `.claude/rules/testing.md`'s
"end-to-end tests are load-bearing" convention, not a directly-seeded
store): `authenticate -> delete` familiarizes "delete" independently;
`read -> update` is this actor's normal path; `read -> delete` (never
observed) fires `transition_deviation` without `categorical_novelty`
also firing, proving the signal isolates *transition* novelty from
*destination* novelty; the familiar `read -> update` path carries no
`transition_deviation`.

## Benchmarks

`internal/baseline/baseline_bench_test.go` (+1):
`BenchmarkObserveTransition` — a strictly-advancing clock and
alternating fingerprint pair, the worst case that actually exercises
`recordPredecessor`'s copy-on-write (`BenchmarkObserve`'s existing
fixed-clock benchmark never does, since its steady-state calls never
satisfy the ordering guard — documented in its own updated comment).

`internal/anomaly/anomaly_bench_test.go` (+1):
`BenchmarkScoreTransitionDeviation` — a mature fingerprint with a
near-`maxPredecessors`-sized `PredecessorCounts` map, scored against an
unseen predecessor (the signal actually fires).

Measured (Apple M3 Pro; see [PERFORMANCE.md § v0.6 task
025](../PERFORMANCE.md#v06-task-025-sequence-analysis-foundation) for
the full table and discussion):

| Benchmark | Before v0.6 | After v0.6 |
|---|---|---|
| `BenchmarkObserve` (no transition recorded) | 231.6 ns/op, 672 B/op, 3 allocs | 222.7 ns/op, 672 B/op, 3 allocs — unchanged |
| `BenchmarkObserveTransition` (new) | — | 377.1 ns/op, 1344 B/op, 6 allocs |
| `BenchmarkInMemoryObserveSameKey` (pre-existing, `internal/store`) | 412.7 ns/op, 672 B/op, 3 allocs | 573.6 ns/op, 910 B/op, 4 allocs |
| `BenchmarkInMemoryObserveDistinctKeys` (pre-existing, `internal/store`) | 173.2 ns/op, 672 B/op, 3 allocs | 241.4 ns/op, 928 B/op, 5 allocs |
| `BenchmarkScoreKnownFamiliar` | 80.18 ns/op, 0 B/op, 0 allocs | 97.74 ns/op, 0 B/op, 0 allocs |
| `BenchmarkScoreTransitionDeviation` (new) | — | 163.0 ns/op, 160 B/op, 3 allocs |
| `BenchmarkEngineAnalyze` (end-to-end, **read-only**, default config) | 462.3 ns/op, 456 B/op, 17 allocs | 481.0 ns/op, 456 B/op, 17 allocs — same allocation profile |

**Read path (`Engine.Analyze`) allocation profile is unchanged** —
confirmed directly (`BenchmarkScoreKnownFamiliar`'s already-familiar
case is a map read plus an early-return zero-value `Signal`, never
appended to `Contributors`: 0 B/0 allocs either way), not merely
because a benchmark's fixed clock happens to avoid the new code.

**Write path (`Engine.Observe`) allocation profile genuinely
increased** — `+1` alloc / `~+240` B per call, once a real transition
is actually being recorded, reproduced identically across three
independent full test runs on the pre-existing `internal/store`
benchmarks (which use a real, advancing `time.Now()` — unlike
`BenchmarkObserve`'s fixed clock, which never exercises this cost at
all, a documented gap that `BenchmarkObserveTransition` (new) exists
to close honestly). This is `recordPredecessor`'s copy-on-write — the
same immutability cost `Baseline.Fingerprints` itself already pays on
every `Observe`, now paid a second time for `PredecessorCounts`. Not
optimized away: numbers remain sub-microsecond and sub-kilobyte
throughout, and the alternative (in-place mutation) would be unsafe
(see [ADR 0010](adr/0010-bounded-process-local-sequence-state.md)).

## Documentation

- [docs/sequence-analysis.md](../sequence-analysis.md) (new): the full
  design writeup.
- [docs/adr/0010-bounded-process-local-sequence-state.md](adr/0010-bounded-process-local-sequence-state.md)
  (new): the state-model decision.
- [docs/DOMAIN.md](DOMAIN.md): new "Sequence-aware detection" section.
- [docs/SECURITY.md](SECURITY.md): new "Sequence state" threat entry
  (memory exhaustion, high-cardinality identities, cross-actor
  contamination, out-of-order events, sensitive history retention,
  process-local scope).
- [docs/PERFORMANCE.md](PERFORMANCE.md): new benchmark results.
- [docs/ROADMAP.md](ROADMAP.md): `v0.6` scope finalized into slices;
  this task marked done; `v0.6` marked in progress (not shipped).
- [trustvian-project-spec.md](../trustvian-project-spec.md) § 6: status
  callout, matching the convention § 17/§ 18 already use.
- [README.md](../README.md): `v0.6` mentioned as in-progress work.
- [CHANGELOG.md](../CHANGELOG.md): new `## Unreleased` section
  (this repository's first — see its own note on why, given the
  established one-heading-per-tag convention for `v0.1.0`–`v0.5.0`).

## Acceptance Criteria

- `go test ./... -race -count=1` green at the repository root,
  including every new test above, and every pre-existing test
  unmodified and still passing.
- `go test -bench=. -benchmem ./...` runs and reports the numbers in
  the table above; `BenchmarkEngineAnalyze`'s allocation profile
  (`B/op`, `allocs/op`) is byte-for-byte unchanged from before this
  task.
- `gofmt -l .`, `go vet ./...` clean.
- No new dependency (`git diff go.mod go.sum` empty).
- `processor/go.mod`'s dependency on the released core is unaffected
  (this task touches no public API `processor/` could depend on).
- No `SequenceStore`/`SequenceKey`/`SequenceAnalyzer` type exists
  anywhere, public or internal.
- `TestAnalyzeTransitionDeviationEndToEnd` proves the full
  `Event -> Engine -> transition_deviation signal -> Result` path
  through the real, gated learning loop — not a synthetic,
  directly-seeded baseline.
