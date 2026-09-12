# 027 — Bounded n-gram Detection (Fixed 3-gram)

**Milestone:** v0.6 · **Depends on:** [025](025-sequence-analysis-foundation.md)
(the transition foundation) and [026](026-transition-rarity.md) (the
orientation/minimum-support/naming pattern this task repeats one level
up) · **Blocks:** Markov transition scoring (illustrative, unscoped —
[docs/ROADMAP.md § v0.6](../ROADMAP.md#v06--behavioral-detection-depth))
· **Independent of:** `v0.5` (`config`/`alert`) — nothing here touches
Policy or Alert configuration.

## Objective

Let Trustvian detect higher-order behavioral novelty a pairwise signal
structurally cannot see: `A -> B` and `B -> C` can each be completely
familiar on their own, while the specific sequence `A -> B -> C` has
never occurred. Ship exactly one fixed-length detector — a 3-gram — not
a configurable `n`. See
[ADR 0012](../adr/0012-bounded-trigram-behavioral-context.md) for the
full statistical definition, orientation proof, and bounding argument.

## Why

`docs/ROADMAP.md` § v0.6 named this precisely as the next unscoped
slice after task 026: *"n-gram behavioral detection — generalizing
`LastFingerprintID` (one step) into a small, still explicitly bounded
ring buffer of the last N fingerprints."* The canonical example:
`authenticate -> read_customer` is normal; `read_customer ->
export_customer` may also be normal, elsewhere; but `authenticate ->
read_customer -> export_customer`, as one complete sequence, may never
have happened for this actor — a fact task 025/026's own signals cannot
express, since each only ever compares one event against its single
immediate predecessor.

## Scope

```text
Baseline.Observe (write path)
      ↓
PreviousFingerprintID/LastFingerprintID window shifts (ordering-guarded)
      ↓
FingerprintStats.TrigramCounts (destination) / TrigramContinuationTotal
(immediate predecessor) record the 3-gram (both bounded)

anomaly.Score (read path)
      ↓
ngramDeviationSignal reads TrigramCounts
ngramRaritySignal reads TrigramCounts + TrigramContinuationTotal
      ↓
Signal{Name: "ngram_deviation"/"ngram_rarity", ...} — noisy-OR combined, opt-in
      ↓
Anomaly.Contributors / Anomaly.Score  (existing Result shape, unchanged)
```

- **`internal/baseline`**: `Baseline` gains `PreviousFingerprintID
  string` (the fingerprint two steps back); `FingerprintStats` gains
  `TrigramCounts map[TrigramKey]uint64` (on the destination, bounded at
  `maxTrigramPredecessors = 64`) and `TrigramContinuationTotal
  map[string]uint64` (on the immediate predecessor, keyed by
  grandparent, independently bounded at the same 64). `TrigramKey` is a
  new, plain two-field comparable struct
  (`{First, Second string}`), with `MarshalText`/`UnmarshalText` solely
  for `store.FileStore`'s `encoding/json` persistence.
- **`internal/anomaly`**: `Config` gains `NGramWeight float64`
  (defaults to `0`), `MinNGramObservations uint64` (defaults to `20`,
  a *separate* field from `MinTransitionObservations` — see ADR 0012
  for why reuse was rejected), and `NGramRarityWeight float64`
  (defaults to `0`); `Score` gains two new signals,
  `ngram_deviation`/`ngram_rarity`, via `ngramDeviationSignal`/
  `ngramRaritySignal`, evaluated only when both `LastFingerprintID` and
  `PreviousFingerprintID` are populated and the current event validly
  follows `LastFingerprintTime`.
- **Zero changes** to `engine.go`, `options.go`, `internal/store`,
  `internal/trust`, `internal/policy`, `alert`, `config`,
  `cmd/trustvian`, `processor/` — identical reuse discipline to tasks
  025/026.

## Non-Goals

- **No configurable `n`.** A fixed 3-gram only — see ADR 0012 § "Why
  3-grams first, not arbitrary n." A future generalization is possible
  without revisiting this task's decisions, but is not built
  speculatively here.
- **No Markov model, no smoothing, no transition matrix.** `frequency`
  is an unsmoothed empirical ratio, exactly like task 026's own
  `transition_rarity` — see ADR 0012 § "Future extension."
- **No ML, ML-adjacent library, or external anomaly service.**
- **No new public API.** No `SequenceStore`, `SequenceKey`,
  `NGramStore`, `NGramModel`, or `SequenceHistory` type anywhere,
  public or internal.
- **No CLI, Collector, or config-schema changes.** Activation is
  Go-SDK-only, via `WithAnomalyConfig` — identical to tasks 025/026's
  own scope boundary, and not required by the current roadmap.
- **No raw `Event`/payload retention, no unbounded maps, no separate
  sequence database.** Both new maps are independently bounded (see
  ADR 0012), and the only new scalar field
  (`PreviousFingerprintID`) is a single string, not a ring buffer.
- **No TTL / active eviction / distributed state.** Process-local,
  matching ADR 0010's own reasoning, restated for this task's own new
  state in [SECURITY.md § Sequence state](../SECURITY.md#sequence-state).

## Technical Requirements

- Every pre-existing test (tasks 001–026) passes completely
  unmodified — proof this task does not change any prior `Score`/
  `Decision` output for a caller that hasn't set the two new weights.
- `TrigramCounts`/`TrigramContinuationTotal` extend the existing
  copy-on-write immutability discipline; both are proven immutable by
  dedicated tests.
- `TrigramContinuationTotal` is bounded *independently* of
  `PredecessorCounts`'s own cap — a genuine correctness subtlety this
  task's own review caught (see ADR 0012's "Why `TrigramContinuationTotal`
  needed its own bound" section) and proved by
  `TestBaselineObserveTrigramContinuationTotalIsBounded`.
- An out-of-order/backdated event contributes no 3-gram information in
  either direction and does not shift the history window — mirroring
  task 025/026's identical guard, one level further back.
- Equal timestamps are handled deterministically (identical to the
  existing `now.After(...)` guard, not a new special case) — proven by
  `TestBaselineObserveEqualTimestampsDoNotAdvanceHistory`.
- Repeated fingerprints (`A -> A -> A`) are valid 3-grams, not
  deduplicated — proven by `TestBaselineObserveRepeatedFingerprintTrigram`.
- `NGramWeight`/`NGramRarityWeight` default to `0` — `DefaultConfig()`
  and any existing caller-constructed `Config` literal that doesn't set
  them get byte-for-byte unchanged `Score` output.
- `TrigramKey`'s `MarshalText`/`UnmarshalText` round-trip correctly
  through a real `store.FileStore` write-then-reopen cycle, not just in
  isolation — proven by `TestFileStoreSurvivesRestartWithTrigramState`.

## Tests

`internal/baseline/baseline_test.go` (+13):
`TestBaselineObserveTracksPreviousFingerprintIDWindow`,
`TestBaselineObserveNoTrigramBeforeThirdObservation`,
`TestBaselineObserveRecordsTrigram`,
`TestBaselineObserveRecordsRepeatedTrigram`,
`TestBaselineObserveRepeatedFingerprintTrigram`,
`TestBaselineObserveOutOfOrderEventDoesNotRecordOrCorruptTrigram`,
`TestBaselineObserveEqualTimestampsDoNotAdvanceHistory`,
`TestBaselineObserveTrigramCountsIsImmutable`,
`TestBaselineObserveTrigramContinuationTotalIsImmutable`,
`TestBaselineObserveTrigramCountsIsBounded`,
`TestBaselineObserveTrigramContinuationTotalIsBounded`,
`TestTrigramKeyTextRoundTrip`.

`internal/store` (+2): `TestFileStoreSurvivesRestartWithTrigramState`
(`file_test.go` — the real `encoding/json` persistence round trip),
`TestInMemoryObserveConcurrentTrigramTracking` (`store_test.go` — the
concurrency proof, run under `-race`).

`internal/anomaly/anomaly_test.go` (+9):
`TestDefaultConfigNGramWeightIsOptIn`,
`TestScoreNoNGramSignalBeforeTrigramHistoryExists` (the cold-start
table: events #1/#2 have no history, #3 is the first complete 3-gram),
`TestScoreNGramDeviationFamiliarTrigram`,
`TestScoreNGramDeviationNovelTrigram`,
**`TestScoreNGramDeviationDetectsNovelTrigramDespiteFamiliarPairwiseTransitions`**
— the task's own mandatory critical-semantic proof: both pairwise hops
familiar, complete 3-gram never observed, `ngram_deviation` still
fires — proving this detector adds genuine information beyond tasks
025/026,
`TestScoreNGramRarityOrdering` (900/90/10 worked example, strict
`common < uncommon < rare` ordering),
`TestScoreNGramRarityColdStart` (minimum-support table),
`TestScoreMatchesDocumentedFormulaForNGramRarity` (exact arithmetic),
`TestScoreNGramRarityNeverExceedsBounds` (property test),
`TestScoreCombinedSequenceSignalsRemainBounded` (§27's required
proof: transition + ngram deviation firing together stays finite and
in `[0,1]`).

`engine_test.go` (+4): `TestAnalyzeNGramEndToEnd` (the task's own
central integration proof, built through the real, gated
`Analyze`+`Observe` loop, reproducing the canonical
authenticate/read_customer/export_customer example — see its own doc
comment for why warm-up uses `NGramWeight`'s default of `0`, mirroring
every other opt-in signal's existing precedent, via a second `Engine`
sharing one `Store`), `TestAnalyzeNGramCrossActorIsolation`,
`TestAnalyzeNGramScoresBeforeLearning` (mirrors `TestAnalyzeIsReadOnly`),
`TestObserveNGramLearnsOnlyFromEligibleDecisions` (the poisoning-guard
regression, using the task's own named scenario: normal
`authenticate -> read -> update` vs. malicious, BLOCKed
`authenticate -> export -> delete`).

All of the above run under `go test ./... -race -count=1`.

## Benchmarks

`internal/baseline/baseline_bench_test.go` (+1): `BenchmarkObserveTrigram`
— a strictly-advancing clock cycling through 3 distinct fingerprints,
so every call (after the first two) completes a valid 3-gram.

`internal/anomaly/anomaly_bench_test.go` (+2):
`BenchmarkScoreNGramDeviation`, `BenchmarkScoreNGramRarity` — mirroring
`BenchmarkScoreTransitionDeviation`/`BenchmarkScoreTransitionRarity`'s
own worst-case shape (a `TrigramCounts`/`TrigramContinuationTotal` map
sized near the bound, scored against a case that actually fires).

`engine_bench_test.go` (+1): `BenchmarkEngineAnalyzeNGram` — the same
steady-state pipeline as `BenchmarkEngineAnalyze`, with both new
weights enabled.

Measured (Apple M3 Pro; see [PERFORMANCE.md § v0.6 task
027](../PERFORMANCE.md#v06-task-027-bounded-ngram-detection) for the
full table, a same-machine A/B against task 026 in a disposable
worktree, and full discussion):

| Benchmark | Before (task 026) | After (task 027) |
|---|---:|---:|
| `BenchmarkObserveTransition` | 435.9 ns/op, 1,408 B/op, 6 allocs | 688–702 ns/op, 2,064 B/op, 10 allocs |
| `BenchmarkObserveTrigram` (new) | — | 758–772 ns/op, 2,512 B/op, 11 allocs |
| `BenchmarkScoreNGramDeviation` (new) | — | 289.9–291.5 ns/op, 224 B/op, 4 allocs |
| `BenchmarkScoreNGramRarity` (new) | — | 660–670 ns/op, 672 B/op, 10 allocs |
| `BenchmarkEngineAnalyze` | 512.6–518.8 ns/op, 456 B/op, 17 allocs | 579.7–588 ns/op, 456 B/op, 17 allocs |
| `BenchmarkEngineAnalyzeNGram` (new) | — | 584–588 ns/op, 456 B/op, 17 allocs |

**`Engine.Analyze`'s allocation profile stays byte-for-byte unchanged**
(`456 B/op, 17 allocs/op`), but latency grows by ~65–70ns/call — both
new signals run unconditionally whenever a complete 3-gram history
exists, the same "always-compute, weight-gate the contribution"
pattern every prior signal already follows. Reported, not hidden — see
PERFORMANCE.md for the full accounting, including two pre-existing
benchmarks (`BenchmarkScoreTransitionDeviation`,
`BenchmarkScoreKnownFamiliar`) whose fixtures now incidentally also
exercise the new signals as a side effect of `PreviousFingerprintID`
existing at all, and how each was handled.

## Documentation

- [docs/adr/0012-bounded-trigram-behavioral-context.md](../adr/0012-bounded-trigram-behavioral-context.md)
  (new): the statistical-definition and bounding-argument ADR.
- [docs/sequence-analysis.md](../sequence-analysis.md): new "Bounded
  3-gram detection" section.
- [docs/DOMAIN.md](../DOMAIN.md): "Sequence-aware detection" section
  extended to cover `ngram_deviation`/`ngram_rarity`.
- [docs/SECURITY.md](../SECURITY.md): "Sequence state" section
  extended with the cardinality/counter/cold-start/poisoning
  considerations specific to the two new bounded maps.
- [docs/PERFORMANCE.md](../PERFORMANCE.md): new benchmark results.
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md): confirmed no change
  needed (no new package, no new pipeline stage, no dependency-direction
  change) — verified explicitly, not assumed.
- [docs/ROADMAP.md](../ROADMAP.md): task 027 marked done under `v0.6`;
  Markov transition scoring named as the next illustrative, unscoped
  slice.
- [trustvian-project-spec.md](../../trustvian-project-spec.md) § 6:
  status callout extended to name higher-order sequence detection
  alongside transition novelty/rarity.
- [README.md](../../README.md): `v0.6` progress line updated.
- [CHANGELOG.md](../../CHANGELOG.md): new `## Unreleased` → `### Added`
  entry.

## Acceptance Criteria

- `go test ./... -race -count=1` green at the repository root,
  including every new test above, and every pre-existing test
  (tasks 001–026) unmodified and still passing.
- `go test -bench=. -benchmem ./...` runs and reports the numbers in
  the table above; `BenchmarkEngineAnalyze`'s allocation profile
  (`B/op`, `allocs/op`) stays byte-for-byte unchanged from task 026.
- `gofmt -l .`, `go vet ./...` clean.
- No new dependency (`git diff go.mod go.sum` empty).
- `processor/go.mod` unaffected — verified with `GOWORK=off go build
  ./... && GOWORK=off go test ./... -race` inside `processor/`.
- No `SequenceStore`/`SequenceKey`/`NGramStore`/`NGramModel`/
  probability-named type exists anywhere, public or internal.
- `TestAnalyzeNGramEndToEnd` proves the full `Event -> Engine ->
  ngram_deviation signal -> Result` path through the real, gated
  learning loop — not a synthetic, directly-seeded baseline.
- `TestScoreNGramDeviationDetectsNovelTrigramDespiteFamiliarPairwiseTransitions`
  proves this detector adds genuine information beyond tasks 025/026.
