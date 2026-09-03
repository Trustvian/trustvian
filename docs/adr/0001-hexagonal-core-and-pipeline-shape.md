# 0001 — Hexagonal core, one package per pipeline stage

## Context

Trustvian's core job is a fixed sequence — Event → Features →
Fingerprint → Baseline → Anomaly → Trust → Policy → Decision — that
must stay independent of any specific telemetry source, storage
technology, or deployment environment (CLAUDE.md: no OpenTelemetry,
PostgreSQL, Redis, Kafka, Kubernetes, or Control/Cloud dependency in
the core). The engine also needs to be embeddable as a library first,
not built service-first.

## Decision

Each pipeline stage is its own small package under `internal/`, with a
single pure function at its center (`features.Extract`,
`fingerprint.Compute`, `anomaly.Score`, `trust.Compute`,
`policy.Evaluate`). `internal/baseline` is the one stateful domain
type, kept immutable-value-with-copy-on-write rather than a
mutex-guarded struct; concurrency-safe storage is a separate concern
(`internal/store`). A single composition root (`Engine`, root package)
wires the stages together and is the only place that constructs all of
them. No stage imports a later one; see
[ARCHITECTURE.md § dependency direction](../ARCHITECTURE.md#dependency-direction)
for the verified import graph.

## Alternatives considered

- **A monolithic `Engine.Analyze` method** doing all seven steps
  inline. Rejected: makes each step untestable in isolation and
  invites the stages to reach into each other's internals instead of
  communicating through explicit value types.
- **A generic pipeline/middleware abstraction** (`[]Stage` with a
  common interface, chained dynamically). Rejected as premature: there
  is exactly one pipeline shape and no current need to reorder or
  swap stages at runtime; CLAUDE.md explicitly warns against
  abstractions without a real consumer.
- **Event-bus / pull-based processing** (stages subscribe to a shared
  bus). Rejected: the engine is synchronous and push-based
  (`Analyze(ctx, event)` returns a `Result`); a bus would add
  goroutines and buffering with no current requirement driving it.

## Consequences

- Every stage is independently testable and benchmarkable (see
  [PERFORMANCE.md](../PERFORMANCE.md)) without constructing the whole
  `Engine`.
- Composing a different pipeline order or skipping a stage is not
  supported without a new composition root — acceptable, since the
  pipeline shape is a stated product invariant, not a variable.
- Adding a new stage (e.g. a future sequence/n-gram anomaly detector)
  means adding a new package plus one `Engine.Analyze` call site, not
  restructuring existing packages.
