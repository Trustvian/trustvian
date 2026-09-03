# 003 — Baseline v2: Persistence, Staleness, Freeze

**Status: implemented.** Persistent `store.FileStore`,
`FingerprintStats.IsStale`, and `store.Freezer` all exist, are tested
under `-race`, and are benchmarked — see
[ADR 0006](../adr/0006-file-backed-persistent-store.md) and
[PERFORMANCE.md](../PERFORMANCE.md).

**Milestone:** v0.1 · **Depends on:** none · **Blocks:** none ([004](004-anomaly.md)
does not actually depend on this task — see the correction in
004's own header; the two tasks turned out to be independent).

## Objective

Close the three concrete Baseline gaps that block a genuinely useful
`v0.1`: no persistence (documented everywhere as *the* headline
limitation — see [ADR 0004](../adr/0004-narrow-store-port-in-memory-only.md)),
no explicit staleness handling despite already tracking the data
needed for it (`LastObserved`), and no way to stop learning without
losing history (a "freeze" — useful during an active investigation, so
an analyst can inspect a baseline without it continuing to drift while
they work).

## Why

"Baseline expiration" and "baseline drift" are named directly in the
roadmap brief; both already have partial answers in the current
design (`Baseline.Observe`'s EWMA absorbs *drift* by construction —
see [DOMAIN.md § Baseline](../DOMAIN.md#baseline)) but *expiration*
(deciding a fingerprint's history is stale enough to discount or
distrust) has no implementation at all — `LastObserved` is recorded
and never read. Persistence is the single most-cited gap across
`README.md`, `docs/ROADMAP.md` (previous version), and every ADR that
mentions `Store` — this task is where that debt actually gets paid,
not deferred again.

## Scope

1. **Persistent `Store`.** A new `internal/store` implementation
   (e.g. `store.FileStore` — a JSON snapshot on disk, periodically or
   on-shutdown flushed; exact mechanism decided during implementation,
   but must satisfy the existing two-method `Store` interface
   unchanged) implementing `internal/store.Store`. No new dependency —
   the standard library's `encoding/json` and `os` are sufficient;
   explicitly do not reach for BoltDB/SQLite/etc. unless the simple
   version proves inadequate.
2. **Staleness.** A read-time helper (on `FingerprintStats` or a new
   small method) that reports whether a fingerprint's `LastObserved`
   is older than a configurable threshold. `internal/anomaly` (or a
   caller) can then choose to discount `Confidence` for stale entries
   — the actual consumption of this signal may land in 004 alongside
   the frequency signal, but the Baseline-side capability is this
   task's scope.
3. **Freeze.** A `Baseline`-level (or `Store`-level) flag that makes
   `Observe` a no-op for a frozen key while `Get` still returns full
   history. Exposed via a new, narrow `Store` method or a wrapping
   decorator — decide during implementation which keeps `Store`
   narrower (see Non-Goals).

## Non-Goals

- No Redis/PostgreSQL/any external database — explicitly excluded per
  the roadmap principles unless justified, and a file-backed store is
  sufficient for the stated need (a single-process, single-tenant
  engine).
- No automatic expiration/deletion of stale baselines — staleness is
  surfaced as a signal for downstream consumers to weigh, not acted on
  unilaterally by `internal/baseline` itself (matches the existing
  design philosophy: `Baseline` "only counts," it doesn't decide
  policy).
- No time-based *seasonality* (day-of-week/hour-of-day patterns) —
  that's `v0.3`'s tentative scope, not this task's; conflating the two
  risks turning a bounded persistence task into an open-ended
  statistical-modeling one.
- Freeze must not become a generic "pause the whole engine" feature —
  scoped strictly to one `baseline.Key` at a time.

## Technical Requirements

- `FileStore` (or chosen name) implements `internal/store.Store`
  exactly — no interface changes, so `internal/anomaly`,
  `internal/trust`, `internal/policy` remain completely unaware of the
  change, proving [ADR 0004](../adr/0004-narrow-store-port-in-memory-only.md)'s
  design held up.
- Concurrency-safe to the same standard as `InMemory` — sharded or
  otherwise, verified under `-race`.
- Crash-safety is a documented, not necessarily perfect, property: state
  the actual guarantee (e.g. "flushed on `Engine`/`Store` shutdown and
  at a configurable interval; data since the last flush is lost on an
  unclean process exit") rather than silently overselling durability.
- Freeze state itself must be explicit and queryable (a caller can ask
  "is this key frozen"), not just enforced silently.

## Tests

- `FileStore` gets the same test shape as `InMemory`
  (`internal/store/store_test.go`'s existing tests, parameterized or
  duplicated): missing-key behavior, observe-then-get, snapshot
  isolation, concurrent same-key/distinct-key correctness under
  `-race`.
- A restart test: write via one `FileStore` instance, construct a new
  one against the same path, confirm data survives.
- A freeze test: `Observe` on a frozen key is a no-op (`Baseline`
  unchanged, `learned` false or an explicit sentinel — decide
  signature during implementation), `Get` still returns full prior
  history.
- A staleness test: a fingerprint with `LastObserved` beyond the
  threshold is correctly reported stale; one within it is not.

## Benchmarks

- `BenchmarkFileStoreObserve` (same/distinct key, mirroring
  `internal/store/store_bench_test.go`'s existing `InMemory`
  benchmarks) — document how much slower persistence is than the
  in-memory baseline; this is expected and fine, but must be measured,
  not assumed.
- Confirm `InMemory`'s existing benchmarks are unaffected (no shared
  code path regression).

## Documentation

- [ADR 0006](../adr/) (new): why a file-backed store and not
  Redis/Postgres/BoltDB, and the exact durability guarantee chosen.
- [ARCHITECTURE.md § storage boundary](../ARCHITECTURE.md#storage-boundary):
  update to describe the new implementation as available (not just
  "additive and unbuilt").
- [ROADMAP.md](../ROADMAP.md): move "persistent `Store`" from
  "Future research"/gaps into "Implemented" once shipped.
- [SECURITY.md](../SECURITY.md): note the durability guarantee's
  security-relevant implication, if any (e.g. does an unclean shutdown
  followed by restart create any poisoning-adjacent risk from stale
  vs. missing data — likely not, but state it rather than leave it
  implicit).

## Acceptance Criteria

- `go test ./internal/store/... ./internal/baseline/... -race` green.
- A real restart-survival test passes against an actual file on disk
  (not mocked).
- `Store` interface (`internal/store/store.go`) is unchanged in shape
  — zero ripple into `internal/anomaly`/`trust`/`policy`/`Engine`
  beyond `trustvian.WithStore(...)` wiring.
- Freeze and staleness are both independently testable and tested.
- No new third-party dependency added.
