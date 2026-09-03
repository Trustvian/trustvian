# Performance

## Philosophy

Trustvian is a runtime security engine — `Engine.Analyze` is meant to
sit on a request path, so its cost has to be predictable and small.
CLAUDE.md's stance applies directly: pay attention to allocations, CPU
overhead, lock contention, goroutine lifecycle, and memory growth; use
benchmarks, not guesses; don't prematurely optimize. Every number below
is measured, not estimated — see
[ADR 0005](adr/0005-fingerprint-computed-once-per-analyze.md) for a
concrete example of a real inefficiency this benchmark suite caught
that inline code review had missed.

## Hot paths

In order of how often they run in a typical deployment:

1. `Engine.Analyze` — runs on every event. The full pipeline.
2. `Engine.Observe` — runs on every event whose decision is
   learning-eligible (see [SECURITY.md](SECURITY.md#baseline-poisoning)).
   Its cost is dominated by `Store.Observe`.
3. Everything inside `Analyze` individually: `features.Extract`,
   `fingerprint.Compute`, `Store.Get`, `anomaly.Score`,
   `trust.Compute`, `policy.Evaluate`.

## Measured results

Environment: Go 1.27, darwin/arm64, Apple M3 Pro. Run with
`make bench` or `go test ./... -run '^$' -bench . -benchmem`.
`-12` suffix = `GOMAXPROCS`/parallel benchmark; no suffix = sequential
(`-cpu 1`).

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `features.Extract` | 18.1 | 0 | 0 |
| `fingerprint.Compute` | 161.0 | 120 | 15 |
| `trust.Compute` | 5.1 | 0 | 0 |
| `policy.Evaluate` (rule match) | 21.1 | 0 | 0 |
| `policy.Evaluate` (falls to default) | 36.8 | 0 | 0 |
| `baseline.Observe` (direct, same fingerprint) | 165.8 | 464 | 3 |
| `anomaly.Score` (familiar, no signal fires) | 47.0 | 0 | 0 |
| `anomaly.Score` (novel, every signal fires) | 228.8 | 448 | 5 |
| `store.InMemory.Observe` (same key, sequential) | 292.7 | 432 | 3 |
| `store.InMemory.Observe` (same key, 12-way parallel) | 359.4 | 432 | 3 |
| `store.InMemory.Observe` (distinct keys, sequential) | 296.4 | 432 | 3 |
| `store.InMemory.Observe` (distinct keys, 12-way parallel) | 149.2 | 432 | 3 |
| `store.FileStore.Observe` (same key, sequential) | 3,912,188 | 3,567 | 24 |
| `store.FileStore.Observe` (same key, 12-way parallel) | 3,410,570 | 3,681 | 24 |
| `store.FileStore.Observe` (distinct keys, sequential) | 3,750,412 | 3,696 | 24 |
| `store.FileStore.Observe` (distinct keys, 12-way parallel) | 3,799,162 | 14,456 | 51 |
| `Engine.Analyze` (sequential) | 433.8 | 456 | 17 |
| `Engine.Analyze` (12-way parallel) | 173.8 | 456 | 17 |

## Reading the numbers

**`Engine.Analyze`'s cost is accounted for.** `features.Extract`
(18ns) + `fingerprint.Compute` (161ns, called once) +
`anomaly.Score`'s familiar-path floor (47ns, which itself already
includes the fingerprint map lookup plus the frequency-deviation
branch's map read, subtraction, and `math.Sqrt`, even when the signal
doesn't fire — see [task 004](tasks/004-anomaly.md)) + `trust.Compute`
(5ns) + `policy.Evaluate` (21–37ns) sum to roughly the measured 434ns,
with the remainder attributable to `Store.Get`'s map read and struct
construction for `Result`. There is no unaccounted cost hiding in
`Engine.Analyze` itself — see
[ADR 0005](adr/0005-fingerprint-computed-once-per-analyze.md) for how
this was confirmed by finding and removing a real duplicate
computation.

**The common case is the cheap case, by design.** `anomaly.Score`'s
familiar/no-signal path is zero-allocation (47.0ns — up from 29.8ns
before task 004 added the frequency-deviation branch, entirely from the
extra arithmetic on that path, not allocation); the "everything fires"
worst case (228.8ns, 5 allocs) only pays for `fmt.Sprintf`-built
explanation strings when a signal has actually fired — see
`latencySignal`'s and `frequencySignal`'s doc comments in
[`internal/anomaly/anomaly.go`](../internal/anomaly/anomaly.go). A
brand-new fingerprint has no Baseline entry at all
(`known == false`), so `frequencySignal` structurally cannot fire
there regardless of how many other signals do — the "everything fires"
benchmark's allocation count is unchanged by task 004 for exactly that
reason. Explainability's cost is proportional to how much there is to
explain, not paid unconditionally on every call.

**Sharded locking measurably works.** `store.InMemory.Observe` under
12-way concurrent load on distinct keys (149.2ns) is roughly 2.4×
faster than 12-way concurrent load on the *same* key (359.4ns) — this
is the per-`Key`-sharded lock in `internal/store` doing its job: two
different actors never contend with each other, only concurrent
updates to the same actor's baseline do.

**Persistence costs roughly four orders of magnitude, and that's the
deliberate tradeoff.** `store.FileStore.Observe` (~3.4–3.9 ms/op) is
about 10,000–13,000× slower than `store.InMemory.Observe` (~150–360
ns/op) — entirely attributable to a synchronous `fsync` on every call
(see [ADR 0006](adr/0006-file-backed-persistent-store.md)). This is
large enough that it matters which `Store` a deployment chooses:
`InMemory` for throughput, `FileStore` when surviving a restart is
worth the cost. Note `FileStoreObserveDistinctKeys`'s 12-way-parallel
row shows both higher `ns/op` *and* much higher `B/op`/`allocs/op`
(14,456 B, 51 allocs) than its sequential counterpart (3,696 B, 24
allocs) — this specific benchmark's store grows as concurrent
goroutines each add a new key during the run (see the benchmark's own
doc comment in `internal/store/file_bench_test.go`), so later flushes
in that run are serializing a larger snapshot than earlier ones; it is
not a per-call regression, it's the flush-cost-scales-with-store-size
property `FileStore`'s design accepts, visible in the data.

**`Engine.Analyze` scales under real concurrency.** 433.8ns
sequential vs. 173.8ns at 12-way parallelism reflects that the common
path (`Store.Get`) only takes a read lock and every other stage is a
pure function with no shared mutable state.

## Allocation considerations

- `fingerprint.Compute`'s 15 allocs/op come from `hash/fnv.New64a()`
  (a heap-allocated hasher) and `strconv.FormatUint` (the resulting ID
  string) — a known, accepted cost from the original implementation,
  not yet optimized further since it hasn't shown up as the dominant
  cost in end-to-end benchmarks. A future optimization (reusing a
  hasher, avoiding the string conversion) is possible but unmeasured —
  not claimed here as already done. [Task 002](tasks/002-fingerprint.md)
  added the `fingerprintVersion` marker and `TargetCategory` to the
  hash input (two more `writeField` calls), which is the entirety of
  the increase from the prior 12 allocs/op, 104 B/op, 136.7 ns/op —
  each additional field written through an `io.Writer` interface
  carries its own small conversion/write cost; still not the dominant
  cost anywhere it's measured end-to-end.
- `baseline.Observe` and `store.InMemory.Observe`'s 3 allocs/op are the
  copy-on-write `Fingerprints` map rebuild — a deliberate tradeoff (see
  [ADR 0004](adr/0004-narrow-store-port-in-memory-only.md) and
  `Baseline`'s doc comment): paying a map-rebuild cost on the
  *infrequent, gated* write path (`Observe`) is what makes the *hot,
  frequent* read path (`Get`, called on every `Analyze`) completely
  lock-free after acquiring a read lock, with no defensive copying
  needed. [Task 004](tasks/004-anomaly.md)'s three new
  `FingerprintStats` fields (`IntervalObservations`, `IntervalMean`,
  `IntervalVariance`) grew the struct copied into that map by 24 bytes
  (three more `float64`/`uint64`-sized fields), which is the entirety
  of `baseline.Observe`'s B/op increase (432 → 464); the allocation
  *count* is unchanged (still 3) since it's the same map-rebuild
  shape, just a larger value type.

## Concurrency considerations

- `Engine.Analyze` spawns no goroutines — it's fully synchronous, which
  is what keeps goroutine-leak risk at zero for the current
  implementation (verified: no `go func`/goroutine spawns anywhere in
  non-test code, checked via `grep` across the module).
- `internal/store.InMemory` shards its lock per `baseline.Key`, with a
  brief global lock only for first-time key creation (see
  [ARCHITECTURE.md § storage boundary](ARCHITECTURE.md#storage-boundary)).
  All store and baseline tests run under `go test -race`.
- `store.FileStore` (added for persistence — see
  [ADR 0006](adr/0006-file-backed-persistent-store.md)) keeps this
  property: its synchronous flush-per-`Observe` design was chosen
  specifically to avoid introducing this module's first background
  goroutine (a timer-based flusher was considered and rejected for
  exactly that reason). Re-verified after adding it: still zero
  `go func`/goroutine spawns anywhere in non-test code.

## What's not benchmarked (yet)

Both gaps below are scoped, concrete tasks — see
[`tasks/011-performance.md`](tasks/011-performance.md) — not
open-ended future work.

- `internal/otel.EventFromSpan` — not on Trustvian's own hot path
  today (it's a conversion step a caller runs before `Analyze`, not
  something `Engine` calls), so it hasn't been prioritized. Worth
  adding once the adapter has a real embedding caller.
- Sustained memory growth of `store.InMemory` under an unbounded
  number of distinct actors/fingerprints over a long-running process —
  no eviction/expiration exists yet (see
  [ROADMAP.md](ROADMAP.md)), so this is a known open question, not a
  measured one.
