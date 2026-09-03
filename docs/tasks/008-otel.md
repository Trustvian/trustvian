# 008 — OpenTelemetry Integration v2: Outbound Attributes

**Milestone:** v0.2 · **Depends on:** v0.1 shipped (a stable `Result`
shape to export) · **Blocks:** [009](009-otel-collector.md) (the
Collector processor needs this to actually enrich telemetry)

## Objective

Implement the outbound direction of the OTel boundary: writing a
`Result` back onto a span/attribute set as the six `trustvian.*`
attributes named in the original spec. Today `internal/otel` is
inbound-only (span → `Event`); this task completes the round trip.

## Why

[OPENTELEMETRY.md § Trustvian output attributes](../OPENTELEMETRY.md#trustvian-output-attributes-not-yet-implemented)
already documents these six attributes as designed-but-unimplemented,
explicitly to avoid documentation implying functionality that doesn't
exist. This task is where that gap actually closes — and it's the
concrete prerequisite for [009](009-otel-collector.md): a Collector
processor without an outbound writer has nothing useful to do besides
scoring and discarding the result.

## Scope

- A new function in `internal/otel` (e.g.
  `AttributesFromResult(result trustvian.Result) []attribute.KeyValue`
  — exact signature decided during implementation, noting this
  requires `internal/otel` to depend on the root `trustvian` package
  for `Result`'s type, which is a new, one-directional dependency edge
  worth confirming doesn't create a cycle: `internal/otel` currently
  depends on `event`+`internal/features`; the root package depends on
  `internal/otel`? — **no**, it doesn't today, so this is safe, but
  must be verified with `go list -deps` as part of this task, not
  assumed).
- Map: `trustvian.anomaly.score` ← `Anomaly.Score`,
  `trustvian.trust.score` ← `Trust.Score`, `trustvian.risk.level` ←
  `Trust.Risk`, `trustvian.decision` ← `Decision`,
  `trustvian.fingerprint.id` ← `Fingerprint.ID`.
- Resolve `trustvian.behavior.id`'s undefined meaning (flagged as an
  open question in
  [OPENTELEMETRY.md](../OPENTELEMETRY.md#trustvian-output-attributes-not-yet-implemented)):
  decide during implementation whether it's redundant with
  `fingerprint.id` (and should be dropped from the documented set with
  a rationale) or represents something distinct (e.g. a
  human-readable label vs. the hash ID) and implement accordingly —
  do not implement an attribute whose meaning is still undefined.
- Review semantic-convention mapping completeness in the *inbound*
  direction too, now that real outbound usage exists to test the
  round trip against (span → `Event` → `Result` → attributes) —
  bounded to closing any convention gaps discovered by that round-trip
  testing, not a speculative audit.

## Non-Goals

- No change to the inbound `EventFromSpan` mapping table's existing
  behavior — only additive review.
- No writing attributes onto a *live* span mid-request (this function
  returns attributes for a caller to attach; how/when they're attached
  to a real span is the Collector processor's concern,
  [009](009-otel-collector.md), not this task's).
- No new Trustvian-specific attributes beyond the six already named in
  the spec — if a real need for more emerges, that's a new,
  separately-justified task.

## Technical Requirements

- Pure function, no I/O, matching every other `internal/otel` function's
  existing style.
- Must not introduce an import cycle — verify via `go list -deps ./...`
  as part of this task's acceptance, not just at review time.
- Still the *only* package depending on OpenTelemetry — this task adds
  a dependency from `internal/otel` on the root package, not the
  reverse, and does not change which package(s) import
  `go.opentelemetry.io/otel*`.

## Tests

- `TestAttributesFromResult`: build a `Result` via a real
  `Engine.Analyze` call, convert to attributes, assert each of the
  (resolved) `trustvian.*` keys is present with the expected value.
- A round-trip test: `EventFromSpan` → `Engine.Analyze` →
  `AttributesFromResult`, confirming the whole pipeline composes
  without manual glue code, mirroring the existing
  `TestEventFromSpanRoundTripsThroughPipeline` pattern in
  `internal/otel/otel_test.go`.

## Benchmarks

- `BenchmarkAttributesFromResult` — this is a new hot-path candidate if
  a future Collector processor calls it per-span; benchmark it with
  the same rigor as the inbound adapter, closing part of
  [011](011-performance.md)'s "OTel adapter not benchmarked" gap for
  the outbound side specifically.

## Documentation

- [OPENTELEMETRY.md](../OPENTELEMETRY.md): move the six attributes
  from "not yet implemented" to a normal mapping table entry, with
  `trustvian.behavior.id`'s resolved meaning documented.
- [ROADMAP.md](../ROADMAP.md): mark this item implemented.

## Acceptance Criteria

- `go test ./internal/otel/... -race` green, including the round-trip
  test.
- `go list -deps ./...` confirms no import cycle introduced and that
  `internal/otel` remains the sole OTel-dependent package.
- `trustvian.behavior.id`'s meaning is resolved and documented, not
  left ambiguous.
- Benchmark numbers recorded in [PERFORMANCE.md](../PERFORMANCE.md).
