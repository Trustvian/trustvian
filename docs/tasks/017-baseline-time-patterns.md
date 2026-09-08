# 017 — Baseline & Anomaly Depth: Hour-of-Day Time Pattern

**Milestone:** v0.3 · **Depends on:** v0.2 shipped (this task file is
written now, per [ROADMAP.md § v0.3](../ROADMAP.md#v03--baseline--anomaly-depth)'s
own stated trigger: "scoped as a proper task once v0.2 is done") ·
**Blocks:** none

## Objective

Add the one piece of time-based pattern awareness the roadmap brief
names — hour-of-day seasonality — as a new, deterministic, opt-in
anomaly signal: `time_pattern_deviation`. This is the "genuinely
v2-and-beyond statistical depth" v0.3 exists for, scoped to exactly
what a straightforward per-hour EWMA can support, per the roadmap's own
explicit escape hatch ("if it turns out to need anything beyond a
straightforward per-hour-bucket EWMA, it moves to Future Research
instead").

## Why

Today, `internal/baseline` tracks *what* an actor does (via
`Fingerprint`) and *how often/how fast* (via the frequency-deviation
signal, task 004), but nothing about *when*. A fingerprint that has
only ever fired during business hours suddenly firing at 03:00 UTC is
a real, distinct behavioral deviation — orthogonal to novelty, latency,
error rate, and frequency — that nothing currently detects.

## Scope

- Extend `baseline.FingerprintStats` with `HourActivity [24]float64`
  (an EWMA-smoothed distribution over UTC hour-of-day, using a new,
  deliberately slower `hourActivityAlpha` constant — see Technical
  Requirements for why the package's existing `emaAlpha` is the wrong
  rate for this specific signal, discovered empirically via
  `TestFingerprintStatsHourActivityUniformTraffic`, not assumed) and
  `TimePatternObservations uint64` (mirrors `IntervalObservations`'s
  existing cold-start-tracking role — see Technical Requirements for
  why this must be tracked separately from `Count`).
- Add a `time_pattern_deviation` signal to `internal/anomaly.Score`,
  following the exact pattern `latencySignal`/`frequencySignal`
  establish: zero-cost when it doesn't fire, `Detail` only formatted
  when it does.
- Add `Config.TimePatternWeight` (default `0`, shipping opt-in — see
  Non-Goals for why this mirrors task 004's `FrequencyWeight`
  precedent exactly). No new threshold field is needed beyond the
  existing `MinObservations` (see Technical Requirements for why this
  signal reuses that lever rather than adding a second one).

## Non-Goals

- **No day-of-week dimension.** 24 hour-of-day buckets only, not
  24×7=168. Day-of-week roughly triples the state per `FingerprintStats`
  (72 vs. 24 floats) and meaningfully complicates the update rule
  (which day-of-week/hour combination is "the same slot" across weeks)
  for a first version whose own roadmap entry says to stay simple.
  Promoting to day-of-week awareness is future work if hour-of-day
  alone proves insufficient in practice — not pre-built speculatively.
- **No calendar/timezone-aware bucketing.** Hours are UTC-only, matching
  every other timestamp-derived computation in this codebase
  (`Event.Timestamp` is already required to be an absolute instant, not
  a caller-local wall-clock time). An actor whose real-world "business
  hours" cross a UTC day boundary is not specially handled — this is
  the same simplicity tradeoff `Environment` (a plain string, not a
  structured deployment-topology model) already accepts elsewhere.
- **No distinct-day counting.** `TimePatternObservations` counts
  observations, not distinct calendar days observed. A fingerprint that
  matures past `MinObservations` within a single day will show a
  spuriously sharp (but statistically meaningless) hour pattern. This
  is a real, documented limitation of the minimal version, not silently
  ignored — see Documentation. Tracking distinct days would need
  additional state (e.g. a rolling day-set or a distinct-day counter)
  this task deliberately does not add; revisit only if the opt-in
  default proves insufficient once real calibration data exists.
- **No FFT/spectral/seasonal-decomposition methods, no ML.** A
  bounded, deterministic EWMA-per-bucket, exactly per this milestone's
  own "keep it simple and statistical" mandate (CLAUDE.md).
- **No sequence/n-gram/Markov work of any kind** — orthogonal to time
  patterns and explicitly out of v0.3's scope (see
  [ROADMAP.md § v0.3 Non-goals](../ROADMAP.md#v03--baseline--anomaly-depth)).
- **`TimePatternWeight` ships at `0` (opt-in), exactly like
  `FrequencyWeight`.** The single-day-maturity limitation above, plus
  the general "an EWMA-smoothed distribution needs real calibration
  before it's trustworthy" concern that already justified
  `frequency_deviation` shipping inert by default, applies here with
  at least equal force. An operator enables it once they've confirmed
  their own fleet's traffic has genuine, stable hour-of-day structure.

## Technical Requirements

- `TimePatternObservations` must be tracked as its own counter,
  separate from `Count`, for the same reason `IntervalObservations` is
  separate: a `store.FileStore`-persisted `FingerprintStats` loaded
  from a file written *before* this field existed will unmarshal
  `HourActivity` as its zero value (`[24]float64{}`) — every bucket at
  0. Gating the signal on `Count >= MinObservations` alone would treat
  that migrated-but-never-actually-observed-for-time-patterns
  fingerprint as fully mature, and score every single hour as maximally
  anomalous (since every bucket reads 0). Gating on
  `TimePatternObservations >= MinObservations` instead correctly forces
  fresh accumulation after migration, identically to a brand-new
  fingerprint — no explicit persistence version bump or migration code
  needed, matching how task 004 solved the identical problem for
  `IntervalObservations`.
- `HourActivity` uses its own `hourActivityAlpha` (0.02), not the
  package's shared `emaAlpha` (0.2). Every observation updates all 24
  buckets (see the update rule below), but only one of them matches the
  current hour — the other 23 only ever decay — so a bucket that is hit
  exactly once every 24 observations (uniform hour-of-day traffic, the
  "no real pattern" case) swings between a post-hit peak and a
  pre-next-hit trough purely as a function of *when* it happens to be
  read, not because of any real behavioral difference.
  `TestFingerprintStatsHourActivityUniformTraffic` proved this
  concretely: at `emaAlpha`, that swing is wide enough (~0.0009 to
  ~0.20, against a true uniform share of 1/24≈0.0417) that genuinely
  patternless traffic would read as severely time-anomalous depending
  purely on measurement timing. `hourActivityAlpha=0.02` narrows that
  swing to roughly ±0.01 around 1/24 — small in absolute terms, and,
  through `timePatternSignal`'s ratio-based formula, bounded to at most
  ~0.22 in the resulting `[0,1]` signal value (see
  `TestScoreTimePatternDeviation`'s uniform-traffic subtest) rather
  than the ~0.98 the faster rate would have produced. This is a real,
  disclosed tradeoff — slower adaptation to genuine hour-of-day drift —
  not a free correctness fix, and it does not eliminate the residual
  noise entirely; it bounds it to a level `TimePatternWeight`'s
  opt-in-at-zero default absorbs without needing a second mechanism.
- Reuses `cfg.MinObservations` as the maturity gate (not a new,
  dedicated threshold field): the question this signal's cold-start
  gate answers — "is there enough history to trust this fingerprint's
  learned shape" — is the same question `MinObservations` already
  answers for `categorical_novelty`, and hour-of-day structure
  genuinely needs *more* history to be trustworthy than, say,
  `frequency_deviation`'s single-prior-interval bar — so reusing the
  stricter, existing lever is the more conservative and more
  justified choice, not a corner cut.
- Update rule (per observation, UTC hour `h = now.UTC().Hour()`):
  - First observation (`TimePatternObservations == 0`): seed
    one-hot — `HourActivity[h] = 1.0`, every other bucket `0.0`.
  - Subsequent: for every bucket `i` in `0..23`, apply the same
    EWMA-of-indicator update `ErrorRate` already uses —
    `sample := 1.0` if `i == h` else `0.0`;
    `HourActivity[i] += emaAlpha * (sample - HourActivity[i])`.
  - `TimePatternObservations++` always.
- Anomaly value: `uniformShare := 1.0/24.0`;
  `value := 1 - min(HourActivity[h]/uniformShare, 1)` — bounded to
  `[0,1]` by construction (an EWMA-of-indicator average is itself
  bounded to `[0,1]`, and the `min(...,1)` caps the ratio before the
  subtraction). A fingerprint with genuinely uniform hour-of-day
  traffic scores ~0 at every hour (no pattern to violate); one
  concentrated at a specific hour scores near 0 at that hour and near 1
  at hours it has never fired during.
- Zero-cost on the common path: `Score` must not read/compute this
  signal at all when `known == false` or
  `stats.TimePatternObservations < cfg.MinObservations`, matching every
  other signal's discipline.

## Tests

- `internal/baseline`: an EWMA-convergence test proving repeated
  observations at a fixed hour converge `HourActivity[thatHour]`
  toward `1.0` and every other bucket toward `0.0` (mirrors
  `TestFingerprintStatsLatencyConvergesToStableValue`'s shape); a
  first-observation one-hot-seeding test; a test proving
  `TimePatternObservations` increments independently of `Count`
  (trivially true here since both increment together in normal use,
  but the assertion should read the field directly, not infer it).
- `internal/anomaly`: table-driven tests mirroring
  `TestScoreFrequencyDeviation`'s shape — normal hour does not fire
  (build a baseline concentrated at hour H, score an event at hour H);
  novel hour fires strongly (score an event at an hour the baseline
  has ~0 activity for); cold start does not fire (unknown fingerprint);
  below-`MinObservations` does not fire (known fingerprint,
  `TimePatternObservations < MinObservations`); a uniform-traffic
  baseline (observations spread evenly across all 24 hours) stays
  within the documented noise bound regardless of which hour the
  scored event falls on — the "no real pattern to violate" case,
  bounded rather than exactly zero (see Technical Requirements).
  Extend the
  `TestScoreMatchesDocumentedNoisyOrFormula`-style exact-arithmetic
  test with one scenario including a firing `time_pattern_deviation`
  signal.
- A dedicated regression test loading a `FingerprintStats` value with
  `HourActivity` at its zero value but `Count` above `MinObservations`
  (simulating a pre-this-task persisted file) and confirming the signal
  does **not** fire, proving the `TimePatternObservations`-based gate
  — not `Count` — is what's actually enforced.

## Benchmarks

- Re-run `BenchmarkScoreKnownFamiliar`/`BenchmarkScoreNovelWithAllSignals`
  (`internal/anomaly`) after the change; the familiar/no-signal path
  must stay zero-allocation.
- Re-run `BenchmarkObserve` (`internal/baseline`); record the `B/op`
  increase from `HourActivity [24]float64` (192 bytes) — a materially
  larger single addition than any prior task's, so this must be
  measured and documented, not waved through as "probably fine."
- Re-run `store.FileStore` benchmarks; note the larger persisted JSON
  size per fingerprint (24 extra float64 array elements) if it
  measurably affects `Observe`'s already-dominant `fsync` cost (likely
  negligible relative to the existing ~3.7ms/op, but measure rather
  than assume).

## Documentation

- [DOMAIN.md § Baseline](../DOMAIN.md#baseline): document
  `HourActivity`/`TimePatternObservations` alongside the existing
  interval-EWMA description.
- [DOMAIN.md § Anomaly](../DOMAIN.md#anomaly): add `time_pattern_deviation`
  to the signal table, including its opt-in default and the
  single-day-maturity limitation named in Non-Goals.
- [PERFORMANCE.md](../PERFORMANCE.md): updated `anomaly.Score`/
  `baseline.Observe`/`store.FileStore.Observe` numbers.
- [SECURITY.md](../SECURITY.md): a short note under baseline poisoning
  — like the interval EWMA, `HourActivity` is skewable by a single
  allowed-but-unusual-hour observation in the same bounded,
  self-correcting way already documented there; no new threat class,
  cross-referenced rather than re-argued.
- [ROADMAP.md](../ROADMAP.md): mark this task done under `v0.3`.

## Acceptance Criteria

- `go test ./internal/anomaly/... ./internal/baseline/... -race` green,
  including the persisted-zero-value regression test.
- New signal is zero-allocation when it doesn't fire (verified by
  benchmark).
- `TimePatternWeight` defaults to `0` in `anomaly.DefaultConfig()` —
  the signal is computed and reported in `Contributors` but
  contributes nothing to `Score` until an operator opts in, exactly
  like `FrequencyWeight`.
- A fingerprint with uniform hour-of-day traffic never produces a
  signal value above a small, documented bound (~0.3, measured worst
  case ~0.22) at any hour, proven by test — not an unachievable exact
  zero, which a bounded-memory per-bucket EWMA cannot deliver at every
  possible measurement instant (see Technical Requirements).
- Loading a `FingerprintStats` with a zero-value `HourActivity` but a
  mature `Count` does not fire the signal — proven by the dedicated
  regression test, closing the persistence-migration gap explicitly.
