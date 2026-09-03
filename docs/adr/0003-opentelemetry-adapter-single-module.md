# 0003 — OpenTelemetry as an internal adapter, in the same Go module

## Context

CLAUDE.md and the project spec require the core engine to have no
OpenTelemetry dependency, with OTel entering only through an adapter.
Two sub-questions had to be resolved: (a) which package(s) may import
OTel, and (b) whether that adapter needs its own Go module (a
multi-module repository / Go workspace) to keep OTel out of the core's
dependency graph, or whether one module with strict per-package import
discipline is sufficient.

## Decision

`internal/otel` is the only package in this module that imports
`go.opentelemetry.io/otel*`. It depends on the lightweight OTel API
and SDK packages (`otel`, `otel/trace`, `otel/sdk`) — not the
`otelcol-builder` toolchain, which is a materially heavier dependency
tree reserved for building collector components.

The repository stays a **single Go module**. OTel isolation is
enforced at the package/import level (verified via `go list -deps`,
see [ARCHITECTURE.md § dependency direction](../ARCHITECTURE.md#dependency-direction)),
not at the module boundary. A future OpenTelemetry Collector processor
— which does need `otelcol-builder` — is planned as a fully separate
module/repository, so that heavier tree never touches this one at all,
even at the `go.mod` level.

## Alternatives considered

- **Split `internal/otel` into its own Go module now** (e.g. a
  `go.work` multi-module layout), so that consumers importing only the
  core package never see OTel in their own `go.mod`/`go list -m all`
  output. Rejected for now: this is a real, legitimate improvement to
  dependency-graph hygiene, but it's a structural, low-reversibility
  change (splitting a module is disruptive to import paths and
  release/versioning) for an MVP with no external consumers yet
  complaining about it. Package-level isolation already satisfies the
  stated principle ("the core engine must not depend *directly* on
  OpenTelemetry") — no core package imports OTel. Revisit if/when an
  external consumer reports OTel showing up in their build for a
  core-only import.
- **Fold OTel mapping directly into `event` or `internal/features`**
  (e.g. an `Event.FromSpan` method). Rejected: this would make the
  domain vocabulary itself depend on OTel, which is precisely what the
  adapter pattern exists to prevent.
- **Defer the adapter until the Collector processor is built.**
  Rejected: the adapter (span → `Event`) and the processor (a
  deployable OTel component using it) are different-sized deliverables
  with different dependency costs; building the adapter now, in-module
  and lightweight, delivers real value (any Go app already using OTel
  can convert spans today) without pulling in the heavy toolchain.

## Consequences

- `go.mod` lists `go.opentelemetry.io/otel*` as a dependency of the
  *module*, even though the *core packages* never import it — a
  documented, accepted nuance (see
  [OPENTELEMETRY.md](../OPENTELEMETRY.md)), not a violation of the
  "core must not depend on OTel" principle, which is verified at
  import-graph granularity.
- Any consumer that imports only the root `trustvian` package or
  `event` does not compile in OTel code, but `go.mod`/`go.sum` for the
  whole module still lists it as a dependency until/unless a module
  split happens.
- The Collector processor, when built, starts as a clean separate
  module with no risk of dragging `otelcol-builder` into this one.
