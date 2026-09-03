# 0004 — Narrow `Store` port, in-memory implementation only

## Context

`Baseline` data must be readable and updatable by `Engine` without the
pipeline packages knowing or caring how it's persisted (CLAUDE.md:
storage must be replaceable, keep interfaces small and
consumer-owned; do not prematurely implement Redis/PostgreSQL/etc.).

## Decision

`internal/store.Store` exposes exactly two methods, matching
`Baseline`'s real access pattern — read the current snapshot for a
key, apply one incremental observation:

```go
type Store interface {
	Get(ctx context.Context, key baseline.Key) (baseline.Baseline, bool)
	Observe(ctx context.Context, key baseline.Key, fp fingerprint.Fingerprint, vol features.VolatileFeatures, now time.Time) (baseline.Baseline, error)
}
```

The only implementation shipped is `store.InMemory`: a map sharded by
`baseline.Key`, each shard behind its own lock (so unrelated actors
never contend), holding an immutable-value `Baseline` that
`Observe` replaces via copy-on-write rather than mutating in place —
this is what makes a `Get` result safe to read without holding any
lock afterward. No database driver is imported anywhere in this
module.

## Alternatives considered

- **A generic repository interface** (`Save`, `Find`, `Delete`,
  `Query`, ...). Rejected: `Baseline` has exactly one real access
  pattern; a generic CRUD interface would expose operations nothing
  calls and invite ORM-shaped thinking CLAUDE.md explicitly warns
  against.
- **Implement a Redis or Postgres-backed `Store` now**, since
  persistence is an obvious eventual need. Rejected per explicit
  project instruction: do not prematurely implement Redis/PostgreSQL
  before the MVP requires it. The interface is deliberately the seam
  where this becomes additive later.
- **A single global mutex over the whole baseline map**, simpler than
  per-key sharding. Rejected: this is exactly the kind of contention
  hotspot that's expensive to retrofit once callers and benchmarks
  exist around a working interface — see the store benchmarks in
  [PERFORMANCE.md](../PERFORMANCE.md) quantifying the sharding
  benefit.

## Consequences

- Trustvian's baseline does not survive a process restart today — a
  real, documented MVP limitation (see
  [README.md § Limitations](../../README.md#limitations) and
  [ROADMAP.md](../ROADMAP.md)), not a silent gap.
- Adding a persistent `Store` implementation (file-backed, or an
  external store when actually justified) requires no change to any
  pipeline package — only a new type implementing the two-method
  interface and `trustvian.WithStore(...)`.
- Because `Store` is consumer-owned (defined in `internal/store`,
  consumed by `Engine`) rather than defined alongside a hypothetical
  future persistence package, there's no risk of the interface being
  shaped around a specific backend's capabilities instead of the
  pipeline's actual needs.
