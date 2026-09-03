# 004 — Anomaly Detection v2: Frequency Deviation

**Milestone:** v0.1 · **Depends on:** [003](003-baseline.md) (needs
Baseline to track observation-rate data) · **Blocks:** none

## Objective

Add the one anomaly signal named in the roadmap's own example output
that doesn't exist today: frequency deviation. Every other named
signal already has a direct or near-direct equivalent —
`unknown_fingerprint` ≈ `categorical_novelty`, `latency_deviation` and
`error_deviation` exist verbatim, `unusual_target` is partially covered
by `categorical_novelty` (and would gain precision from
[001](001-feature-model.md)'s `TargetCategory`), `unusual_dependency`
and `unusual_operation` are subsumed by `categorical_novelty` (a
`Fingerprint` already encodes operation+target+dependency together).
`unusual_time` and `unusual_sequence` are explicitly `v0.3`/research
territory (see [ROADMAP.md](../ROADMAP.md)), not this task.

## Why

Trustvian currently has no way to say "this actor normally calls this
operation 10 times/minute; it just called it 400 times in the last
minute" — a classic abuse/exfiltration signal (the roadmap's own
"abnormal request frequency" example scenario, [010](010-examples.md)).
`internal/baseline.FingerprintStats` tracks `Count` (all-time) and
latency/error EWMAs, but nothing about *rate*.

## Scope

- Extend `FingerprintStats` (or a sibling structure) with a rate
  estimate — an EWMA over inter-observation intervals, or a
  sliding-window counter; decide during implementation which is
  cheaper and simpler (the codebase's established preference — see
  CLAUDE.md and [ADR 0001](../adr/0001-hexagonal-core-and-pipeline-shape.md)
  — favors the simpler statistical option, an EWMA over intervals,
  matching how latency/error are already tracked, over a windowed
  counter requiring more state).
- Add a `frequency_deviation` signal to `internal/anomaly.Score`,
  following the exact pattern `latencySignal`/`errorSignal` already
  establish: z-score-style deviation from the baseline rate, scaled to
  `[0,1]`, `Detail` only formatted when the signal actually fires (the
  cost-discipline [ADR 0005](../adr/0005-fingerprint-computed-once-per-analyze.md)
  established).
- Add a `Config.FrequencyZThreshold`/`FrequencyWeight` pair, matching
  `LatencyZThreshold`/`LatencyWeight`'s existing shape.

## Non-Goals

- No sequence-aware detection (order of operations) — frequency is a
  *rate* signal, not a *sequence* one; conflating them is exactly the
  kind of premature complexity CLAUDE.md warns against.
- No adaptive/self-tuning thresholds — `Config` fields, set by the
  caller, exactly like every other threshold today.
- No windowed-log storage of raw timestamps — matches Baseline's
  existing "no raw samples retained" EWMA philosophy (see
  [DOMAIN.md § Baseline](../DOMAIN.md#baseline)).

## Technical Requirements

- The new signal must be zero-cost on the common "frequency is normal"
  path — no allocation, no `Detail` formatting — exactly like the
  existing three signals' hot-path discipline (see
  [PERFORMANCE.md](../PERFORMANCE.md)).
- `combine`'s noisy-OR formula is unchanged; the new signal is just
  another entry in `signals`.
- Cold-start interaction: a fingerprint with too few observations to
  have a meaningful rate estimate must not fire this signal — mirrors
  how `latencySignal` already requires `LatencyObservations > 0`.

## Tests

- Table-driven tests in `internal/anomaly/anomaly_test.go` mirroring
  the existing latency/error test shapes: normal rate (no signal),
  spike (signal fires strongly), cold start (signal doesn't fire).
- `internal/baseline` tests for the new rate-tracking field: EWMA
  convergence to a stable rate under repeated identical intervals
  (mirrors `TestFingerprintStatsLatencyConvergesToStableValue`).
- Update `TestScoreMatchesDocumentedNoisyOrFormula`-style exact-formula
  test to include a frequency-contributing scenario, or add a sibling
  test — the existing test's bar (exact arithmetic, not just
  direction) applies to the new signal too.

## Benchmarks

- Re-run `BenchmarkScoreKnownFamiliar`/`BenchmarkScoreNovelWithAllSignals`
  (`internal/anomaly`) after the change; the "familiar" case must stay
  zero-allocation.
- New `BenchmarkObserve` (`internal/baseline`) numbers if the rate
  tracking adds cost to the copy-on-write update path — measure, don't
  assume.

## Documentation

- [DOMAIN.md § Anomaly](../DOMAIN.md#anomaly): add `frequency_deviation`
  to the signal table.
- [PERFORMANCE.md](../PERFORMANCE.md): updated `anomaly.Score`/
  `baseline.Observe` numbers.
- [use-cases.md](../use-cases.md) or [010](010-examples.md)'s output:
  the "abnormal request frequency" example should now actually trigger
  this signal, not just novelty.

## Acceptance Criteria

- `go test ./internal/anomaly/... ./internal/baseline/... -race` green.
- New signal is zero-allocation when it doesn't fire (verified by
  benchmark).
- The "abnormal frequency" example scenario in
  [010](010-examples.md) demonstrably triggers `frequency_deviation`
  in its `Contributors`, with real, run output (not hand-written) —
  matching this documentation set's existing standard.
