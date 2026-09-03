# 0006 — File-backed persistent `Store`, synchronous flush-per-`Observe`

## Context

Baselines not surviving a process restart was, by a wide margin, the
most-cited limitation across this repository's documentation —
`README.md`, `docs/ROADMAP.md`, and [ADR 0004](0004-narrow-store-port-in-memory-only.md)
all named it. [ADR 0004](0004-narrow-store-port-in-memory-only.md)
already committed to *how* a persistent implementation must be added
(implement `Store`'s existing two methods, no interface change) without
building one. [`docs/tasks/003-baseline.md`](../tasks/003-baseline.md)
scoped closing that gap, explicitly ruling out Redis/PostgreSQL/BoltDB
unless a simple file-backed version proved inadequate.

Two implementation questions had to be resolved: what triggers a write
to disk, and how concurrent writers avoid corrupting the file.

## Decision

**`FileStore`** wraps an `*InMemory` internally (composition, not a
parallel reimplementation of sharded locking) and adds one behavior:
every successful `Observe` call, after updating in-memory state via the
wrapped `InMemory`, serializes the *entire* current store contents to
JSON and writes it to disk via a temp-file-plus-`os.Rename` sequence —
atomic on the same filesystem, so a crash mid-write, or a concurrent
reader, never observes a partially-written file.

There is **no background flush goroutine and no timer**. `Get` never
touches disk. Only `Observe` — already the gated, comparatively
infrequent write path (see
[ADR 0004](0004-narrow-store-port-in-memory-only.md)) — pays the disk
cost, and it pays it synchronously and fully before returning: the
durability guarantee is "this specific observation is on disk before
`Observe` returns," not "eventually, on some schedule."

Concurrent `Observe` calls for *different* keys still update in-memory
state fully concurrently (via `InMemory`'s existing per-key sharded
locks); only the disk-write step is serialized, through a single
`flushMu`. Because each flush reads a fresh, complete snapshot of all
current state at the moment it acquires that lock, a queue of pending
flushes always converges to the most recent state — no lost-update
race, even though flush order isn't guaranteed to match `Observe`
completion order.

Freeze state (see `Freezer`, added alongside this ADR — not a `Store`
interface change) is **not persisted**. It is a live, current-process
operational flag, not part of learned behavioral history: "freeze this
actor while I investigate" is a today concern, not a fact that should
outlive a restart.

## Alternatives considered

- **A background goroutine flushing on a timer.** Would let `Observe`
  return without waiting on disk I/O, and would coalesce many rapid
  writes into fewer flushes. Rejected for the MVP: this repository has
  zero goroutines in production code today (verified and stated as a
  deliberate property — see `docs/PERFORMANCE.md`'s concurrency notes),
  and a background flusher introduces the first one, plus the shutdown
  coordination problem it creates (how does a caller know the last
  flush before process exit actually happened?) for a benefit — lower
  `Observe` latency — that the roadmap's own principle ("performance
  before scale," not before correctness) doesn't yet justify paying
  for. `FileStore.Flush()` exists as an explicit, synchronous escape
  hatch if a future caller wants to decouple flush timing from
  `Observe` without needing a goroutine — that's a caller-side choice
  under this design, not one this package forces.
- **Incremental/append-only writes** (write only the changed entry,
  not the full snapshot). Would make `Observe`'s flush cost O(1)
  instead of O(store size). Rejected for the *first* implementation:
  it requires either a WAL-plus-periodic-compaction design or an
  embedded database, both meaningfully more complex than "serialize a
  map to JSON," and the task explicitly scoped "keep the first
  implementation simple... do not reach for BoltDB/SQLite unless the
  simple version proves inadequate." The measured cost (see
  `docs/PERFORMANCE.md`) is the concrete data point that would justify
  revisiting this, not intuition.
- **BoltDB/SQLite/an embedded KV store.** Would solve the O(1)-write
  problem outright. Rejected as premature per the same task scoping
  and the roadmap principle against unjustified infrastructure — no
  new dependency was added; `encoding/json` plus `os` is sufficient for
  what a single-process, single-tenant engine needs today.
- **A generic `Store` decorator for freezing** (wrapping any `Store` to
  add freeze behavior, rather than building it into `InMemory`/
  `FileStore` directly). Considered and rejected in favor of a
  separate `Freezer` interface implemented by each concrete `Store`:
  a decorator would need its own key-level locking independent of the
  wrapped store's, doubling the concurrency surface to reason about,
  for a capability ([ADR-scoped as] per-key, not "pause everything")
  that fits naturally as a few extra fields and methods on the
  existing shard-based state each `Store` already manages.

## Consequences

- `Store`'s interface (`internal/store/store.go`) is unchanged — every
  pipeline package (`internal/anomaly`, `internal/trust`,
  `internal/policy`) and `Engine` remain completely unaware `FileStore`
  or `Freezer` exist. Switching from `InMemory` to `FileStore` is a
  one-line change: `trustvian.WithStore(fileStore)`.
- `Observe`'s cost is now backend-dependent in a way it wasn't before:
  roughly 3.4–3.9 ms/op measured against `FileStore` vs. ~300 ns/op
  against `InMemory` — a difference of about four orders of magnitude,
  entirely attributable to synchronous disk I/O (see
  `docs/PERFORMANCE.md` for full numbers). This is expected and
  accepted for the MVP; a deployment with a high `Observe` rate against
  `FileStore` should expect this cost, not be surprised by it.
- The on-disk format (`fileSnapshotVersion`) is versioned from day one,
  so a future format change has a defined, non-silent path (reject and
  report an unsupported version, rather than misinterpret old data) —
  cheap to add now, the kind of thing that's expensive to retrofit
  later (the same reasoning `Fingerprint` composition versioning
  followed — see [`docs/tasks/002-fingerprint.md`](../tasks/002-fingerprint.md)).
- If `Observe` throughput against `FileStore` becomes a real bottleneck
  for some deployment, the two alternatives ruled out above (background
  flushing, incremental writes) are exactly where to look next — this
  ADR's rejection of them is scoped to "not justified *yet*," not "never."
