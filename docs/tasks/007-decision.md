# 007 — Decision & Explainability

**Milestone:** v0.1 · **Depends on:** none · **Blocks:** none

## Objective

Verify — with a test, not just inspection — that `Result` already
contains everything the roadmap's Decision checklist names, and add
one small convenience: a single human-readable explanation string
covering the *whole* decision (not just the policy stage, which
`Explanation.Reason` already covers).

## Why

Checking `Result` (root package, `result.go`) against the roadmap's
explicit list: final action (`Decision`) ✓, trust score (`Trust.Score`)
✓, risk score (`Trust.Risk`) ✓, anomaly score (`Anomaly.Score`) ✓,
contributing signals (`Anomaly.Contributors`) ✓, matched policy
(`Explanation.RuleName`) ✓, decision reason (`Explanation.Reason`) ✓,
relevant fingerprint (`Fingerprint`) ✓. **This is already fully
implemented.** The roadmap explicitly calls this "a major product
capability" — the gap isn't the data, it's that answering "why did
Trustvian allow/block/challenge this" today requires a caller to read
five different fields and assemble the story themselves (exactly what
`cmd/trustvian/analyze.go`'s `printReport` already does, ad hoc, for
the CLI specifically). This task makes that assembly a reusable SDK
method instead of CLI-only formatting logic.

## Scope

- A `Result.Explain() string` method (root package) producing a
  multi-line, human-readable summary: decision, trust/risk/anomaly
  scores, contributing signals with their details, matched policy rule
  or default reason. This *may* reuse `Trust.Explain()` from
  [005](005-trust-risk.md) internally if that task lands first, or
  stand alone otherwise.
- A regression test asserting, structurally (field presence, not
  fragile exact-output tests), that every item in the roadmap's
  Decision checklist is reachable from `Result` — turning "already
  implemented" from an inspection claim into an enforced contract.

## Non-Goals

- No structured/machine-readable explanation format (JSON schema for
  explanations, etc.) — `Result` itself is already the structured
  form; `Explain()` is specifically the human-readable rendering on
  top of it.
- No change to `cmd/trustvian/analyze.go`'s existing report format —
  it may optionally be refactored to call `Result.Explain()`
  internally once it exists, but that's a nice-to-have cleanup, not
  this task's acceptance bar.

## Technical Requirements

- `Explain()` is pure formatting over existing `Result` fields — no
  new computation, no I/O.
- Output must include every checklist item verbatim enough to be
  greppable in a test (e.g. the literal `Decision` value, not just an
  emoji or abbreviation).

## Tests

- `TestResultExplainContainsAllDecisionFields` (root package): builds
  a representative `Result` (via a real `Engine.Analyze` call against
  a fixture event, not a hand-built struct, so the test exercises the
  real pipeline) and asserts the `Explain()` output contains the
  decision, all three scores, at least one contributing signal's name,
  and the policy reason.
- A second case with an empty `Anomaly.Contributors` (the "nothing
  fired" common case) to confirm `Explain()` degrades gracefully (no
  empty "Detected:" section, no panic on an empty slice).

## Benchmarks

- Not a hot-path method (called for display/logging, not per-event at
  scale) — no benchmark required, matching the "benchmark what's
  actually hot" discipline in
  [`.claude/rules/testing.md`](../../.claude/rules/testing.md).

## Documentation

- [DOMAIN.md](../DOMAIN.md)'s closing paragraph (which already
  describes `Result` as "the complete record") gets a one-line update
  pointing at `Explain()`.
- [Go SDK Guide § Result](../sdk-guide.md#result): add `Explain()` to
  the field/method reference.

## Acceptance Criteria

- `go test .` (root package) green, including the new checklist test.
- `Result.Explain()` output demonstrably covers every item in the
  roadmap's Decision checklist, verified by test assertion, not
  documentation claim alone.
- No change to `Result`'s existing field shape (this task is additive
  only).
