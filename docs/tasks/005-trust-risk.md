# 005 — Trust & Risk Calibration

**Milestone:** v0.1 · **Depends on:** [004](004-anomaly.md) (a new
signal should be included in the calibration matrix) · **Blocks:**
none

## Objective

Validate — not redesign — the existing multiplicative trust formula
against a broader scenario matrix than currently exists, and add a
human-readable explanation helper. This is primarily a review and
documentation task with one small, well-justified code addition.

## Why

`internal/trust.Compute`'s formula
(`TrustScore = IdentityConfidence * (1 - Anomaly.Score*Anomaly.Confidence)
* (1 - ContextRisk)`) is already documented, tested for exact
arithmetic, and confirmed not to reproduce the original spec's own
illustrative example (a deliberate, explained non-goal — see the
formula's doc comment in `internal/trust/trust.go`). What's missing is
breadth: today's tests cover specific hand-picked scenarios, not a
systematic sweep across the input space, and there's no way for a
caller (developer, security engineer, or a future Trustvian Control
dashboard) to get a plain-English sentence explaining *why* a
particular `TrustScore` came out the way it did without reading
`Trust`'s four component fields themselves.

## Scope

- A systematic scenario matrix (property-style or a large table-driven
  test) sweeping `IdentityConfidence`, `Anomaly.Score`,
  `Anomaly.Confidence`, and `ContextRisk` across representative ranges
  (0, low, mid, high, 1) confirming: the formula never produces a value
  outside `[0,1]`, monotonicity holds (increasing any risk input never
  *increases* `TrustScore`; increasing `IdentityConfidence` never
  *decreases* it), and the documented cold-start/dominance properties
  hold across the whole matrix, not just the existing hand-picked
  cases.
- A `Trust.Explain() string` method producing a short, human-readable
  sentence (e.g. `"trust 0.35 (low): identity confidence 0.97,
  anomaly 0.91 at full confidence, context risk 0.10"`), reusing the
  same information already on the `Trust` struct — no new
  computation, just formatting.

## Non-Goals

- No new inputs to the formula (no fifth factor) — the roadmap
  principle "avoid arbitrary formulas" cuts against adding one without
  a concrete, named need.
- No confidence *interval* / uncertainty range on `TrustScore` itself
  (e.g. "0.35 ± 0.1") — `Anomaly.Confidence` already serves as the
  uncertainty signal for the anomaly component specifically; a
  second, separate uncertainty concept for the composed `TrustScore`
  is not justified by any current consumer.
- No per-deployment configurable formula shape (e.g. additive vs.
  multiplicative as a runtime choice) — one formula, documented,
  matching [ADR 0001](../adr/0001-hexagonal-core-and-pipeline-shape.md)'s
  "no plugin/strategy interface without a second real implementation"
  stance.

## Technical Requirements

- `Explain()` must be deterministic and derived purely from `Trust`'s
  existing fields — no hidden state, no I/O.
- The scenario-matrix test should be fast enough to run in the normal
  `go test` loop (not a separate, opt-in fuzz/property suite) — this
  is a bounded, enumerable sweep, not open-ended fuzzing.

## Tests

- The scenario matrix itself, as a new test in
  `internal/trust/trust_test.go`.
- `TestExplain` (or similar): a handful of representative `Trust`
  values produce the expected sentence shape (exact string match for
  at least one case, structural checks — e.g. "contains the risk
  level word" — for the rest, to avoid over-brittle string-exact
  tests).

## Benchmarks

- `BenchmarkCompute` (`internal/trust`, already exists) re-run to
  confirm no regression — this task shouldn't touch `Compute`'s hot
  path at all, so this is a confirmation, not a new measurement effort.
- `Explain()` is not a hot-path method (called for display/logging,
  not per-event at scale) — no benchmark required, matching the
  existing "benchmark what's actually hot" discipline (see
  [`.claude/rules/testing.md`](../../.claude/rules/testing.md)).

## Documentation

- [DOMAIN.md § Trust and Risk](../DOMAIN.md#trust-and-risk): note the
  monotonicity/bounds guarantees now that they're explicitly tested,
  not just implied.
- [Go SDK Guide](../sdk-guide.md): mention `Trust.Explain()` as part of
  the `Result` walkthrough.

## Acceptance Criteria

- `go test ./internal/trust/...` green, including the new scenario
  matrix and `Explain()` tests.
- The scenario matrix explicitly asserts bounds (`[0,1]`) and
  monotonicity — not just spot-checked values.
- No change to `Trust.Compute`'s formula or signature.
