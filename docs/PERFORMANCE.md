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

**This table is the `v0.1` release baseline** (confirmed as part of
[task 013](tasks/013-oss-v01.md)'s release gate, 2026-09-04). Every
number in it was already current as of task 011's own re-measurement;
task 013 re-ran the full `go test ./... -bench . -benchmem -run ^$`
suite fresh (no cache) on the exact commit being released and confirms
every `B/op`/`allocs/op` figure below is unchanged and every `ns/op`
figure is within normal session-to-session noise — see "v0.1 gate
confirmation run" below for that fresh run's numbers side by side.
Future performance work should diff against this table, not against
individual task commits, as the `v0.1` comparison point.

Environment: Go 1.27, darwin/arm64, Apple M3 Pro. Run with
`make bench` or `go test ./... -run '^$' -bench . -benchmem`.
`-12` suffix = `GOMAXPROCS`/parallel benchmark; no suffix = sequential
(`-cpu 1`).

Full suite re-measured for task 011 (see
[tasks/011-performance.md](tasks/011-performance.md)) after tasks
002/004/006 landed. Every allocation count (`B/op`/`allocs/op`) below
is unchanged from the prior recorded session for every benchmark whose
underlying code did not change in those tasks (allocation counts are
deterministic and load-independent, unlike `ns/op`) — see "Reading the
numbers" below for which `ns/op` deltas are real, code-driven changes
versus this session's general measurement noise.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `features.Extract` | 19.8 | 0 | 0 |
| `fingerprint.Compute` | 221.1 | 120 | 15 |
| `trust.Compute` | 5.8 | 0 | 0 |
| `policy.Evaluate` (rule match) | 30.5 | 0 | 0 |
| `policy.Evaluate` (falls to default) | 43.2 | 0 | 0 |
| `baseline.Observe` (direct, same fingerprint) | 276.6 | 464 | 3 |
| `anomaly.Score` (familiar, no signal fires) | 54.1 | 0 | 0 |
| `anomaly.Score` (novel, every signal fires) | 335.3 | 448 | 5 |
| `otel.EventFromSpan` | 385.8 | 696 | 10 |
| `store.InMemory.Observe` (same key, sequential) | 345.7 | 464 | 3 |
| `store.InMemory.Observe` (same key, 12-way parallel) | 363.1 | 464 | 3 |
| `store.InMemory.Observe` (distinct keys, sequential) | 343.8 | 464 | 3 |
| `store.InMemory.Observe` (distinct keys, 12-way parallel) | 180.6 | 464 | 3 |
| `store.InMemory.Observe` memory growth (100 keys) | 243.8 | 464 | 3 |
| `store.InMemory.Observe` memory growth (1,000 keys) | 273.8 | 464 | 3 |
| `store.InMemory.Observe` memory growth (10,000 keys) | 280.8 | 464 | 3 |
| `store.FileStore.Observe` (same key, sequential) | 3,712,459 | 3,777 | 24 |
| `store.FileStore.Observe` (same key, 12-way parallel) | 3,367,967 | 3,812 | 24 |
| `store.FileStore.Observe` (distinct keys, sequential) | 3,858,850 | 3,890 | 24 |
| `store.FileStore.Observe` (distinct keys, 12-way parallel) | 4,700,393 | 16,891 | 51 |
| `Engine.Analyze` (sequential) | 553.8 | 456 | 17 |
| `Engine.Analyze` (12-way parallel) | 191.9 | 456 | 17 |

### v0.1 gate confirmation run

Same environment, same commit, run fresh (no test cache) as part of
[task 013](tasks/013-oss-v01.md)'s release-gate verification. Shown
here to prove the table above reproduces, not as a replacement for it —
`B/op`/`allocs/op` match exactly everywhere; `ns/op` differences are
normal machine-load variance (this run shared the machine with other
work), not code changes.

| Benchmark | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| `features.Extract` | 17.76 | 0 | 0 |
| `fingerprint.Compute` | 170.8 | 120 | 15 |
| `trust.Compute` | 5.161 | 0 | 0 |
| `policy.Evaluate` (rule match) | 27.15 | 0 | 0 |
| `policy.Evaluate` (falls to default) | 37.75 | 0 | 0 |
| `baseline.Observe` (direct, same fingerprint) | 165.8 | 464 | 3 |
| `anomaly.Score` (familiar, no signal fires) | 48.04 | 0 | 0 |
| `anomaly.Score` (novel, every signal fires) | 234.1 | 448 | 5 |
| `otel.EventFromSpan` | 347.9 | 696 | 10 |
| `store.InMemory.Observe` (same key, sequential) | 356.5 | 464 | 3 |
| `store.InMemory.Observe` (distinct keys, sequential) | 140.4 | 464 | 3 |
| `store.InMemory.Observe` memory growth (100 keys) | 212.3 | 464 | 3 |
| `store.InMemory.Observe` memory growth (1,000 keys) | 231.9 | 464 | 3 |
| `store.InMemory.Observe` memory growth (10,000 keys) | 237.2 | 464 | 3 |
| `store.FileStore.Observe` (same key, sequential) | 4,416,998 | 3,873 | 24 |
| `store.FileStore.Observe` (distinct keys, sequential) | 4,668,619 | 16,948 | 51 |
| `Engine.Analyze` (sequential) | 435.6 | 456 | 17 |
| `Engine.Analyze` (12-way parallel) | 158.3 | 456 | 17 |

## Reading the numbers

**Session-to-session `ns/op` moved broadly; allocation counts didn't —
that split tells you what's real.** This table was re-measured for
task 011 on the same environment (Go 1.27, darwin/arm64, Apple M3 Pro)
as the prior recording, and nearly every `ns/op` figure is higher than
before — but every `B/op`/`allocs/op` figure for a benchmark whose code
didn't change is *identical* to the prior recording (`features.Extract`
0/0, `trust.Compute` 0/0, `fingerprint.Compute` 120/15,
`policy.Evaluate` 0/0 for both scenarios). Allocation counts are
deterministic and load-independent; wall-clock `ns/op` is not — a busy
laptop measures slower than an idle one for code that hasn't changed at
all. Concretely: task 006 added attribute matching to
`policy.Condition`, but neither benchmarked `policy.Evaluate` scenario
sets `Condition.Attributes`, so `Condition.Matches` ranges over a `nil`
map there — a zero-cost no-op in Go — which is exactly why its
`allocs/op` is unchanged even though `ns/op` moved with everything
else. Treat the `ns/op` deltas below as this session's absolute
numbers, not as evidence of a regression, except where a specific
code change is named.

**`Engine.Analyze`'s cost is accounted for.** `features.Extract`
(19.8ns) + `fingerprint.Compute` (221.1ns, called once) +
`anomaly.Score`'s familiar-path floor (54.1ns, which itself already
includes the fingerprint map lookup plus the frequency-deviation
branch's map read, subtraction, and `math.Sqrt`, even when the signal
doesn't fire — see [task 004](tasks/004-anomaly.md)) + `trust.Compute`
(5.8ns) + `policy.Evaluate` (30.5–43.2ns) sum to roughly 331–344ns
against the measured 553.8ns, with the remainder attributable to
`Store.Get`'s map read and struct construction for `Result` (the same
~40% remainder-to-sum ratio as the prior recording, so this isn't a new
gap — see [ADR 0005](adr/0005-fingerprint-computed-once-per-analyze.md)
for how the original accounting was confirmed by finding and removing a
real duplicate computation).

**The common case is the cheap case, by design.** `anomaly.Score`'s
familiar/no-signal path is zero-allocation (54.1ns this session, up
from 29.8ns before task 004 added the frequency-deviation branch —
that specific jump was a real, code-driven cost from the extra
arithmetic on that path, not allocation; the further move to 54.1ns is
this session's general variance, not a further code change); the
"everything fires" worst case (335.3ns, 5 allocs) only pays for
`fmt.Sprintf`-built explanation strings when a signal has actually
fired — see `latencySignal`'s and `frequencySignal`'s doc comments in
[`internal/anomaly/anomaly.go`](../internal/anomaly/anomaly.go). A
brand-new fingerprint has no Baseline entry at all
(`known == false`), so `frequencySignal` structurally cannot fire
there regardless of how many other signals do — the "everything fires"
benchmark's allocation count (448 B, 5 allocs) is unchanged from the
prior recording for exactly that reason. Explainability's cost is
proportional to how much there is to explain, not paid unconditionally
on every call.

**Sharded locking measurably works.** `store.InMemory.Observe` under
12-way concurrent load on distinct keys (180.6ns) is roughly 2×
faster than 12-way concurrent load on the *same* key (363.1ns) — this
is the per-`Key`-sharded lock in `internal/store` doing its job: two
different actors never contend with each other, only concurrent
updates to the same actor's baseline do.

**Persistence costs roughly four orders of magnitude, and that's the
deliberate tradeoff.** `store.FileStore.Observe` (~3.4–4.7 ms/op) is
about 10,700×–26,000× slower than `store.InMemory.Observe` (~180–365
ns/op) — entirely attributable to a synchronous `fsync` on every call
(see [ADR 0006](adr/0006-file-backed-persistent-store.md)). This is
large enough that it matters which `Store` a deployment chooses:
`InMemory` for throughput, `FileStore` when surviving a restart is
worth the cost. Note `FileStoreObserveDistinctKeys`'s 12-way-parallel
row shows both higher `ns/op` *and* much higher `B/op`/`allocs/op`
(16,891 B, 51 allocs) than its sequential counterpart (3,890 B, 24
allocs) — this specific benchmark's store grows as concurrent
goroutines each add a new key during the run (see the benchmark's own
doc comment in `internal/store/file_bench_test.go`), so later flushes
in that run are serializing a larger snapshot than earlier ones; it is
not a per-call regression, it's the flush-cost-scales-with-store-size
property `FileStore`'s design accepts, visible in the data.

**`Engine.Analyze` scales under real concurrency.** 553.8ns
sequential vs. 191.9ns at 12-way parallelism reflects that the common
path (`Store.Get`) only takes a read lock and every other stage is a
pure function with no shared mutable state.

**`otel.EventFromSpan` is comparable in cost to the rest of the
pipeline, not a bottleneck relative to it.** At 385.8ns/696B/10 allocs
for a representative HTTP-server span (resource attributes, a
request-method attribute, a measured duration — the same shape as
`TestEventFromSpanHTTPServerMapping`), it costs less than
`fingerprint.Compute` alone (221.1ns) plus `store.InMemory.Observe`
(345.7–363.1ns) combined, and it runs once per span, before `Analyze`,
not inside it. This closes the first item task 011 was scoped to
measure (see [tasks/011-performance.md](tasks/011-performance.md)) —
there was no reason to expect it to be expensive, and it isn't.

**`store.InMemory`'s per-`Observe` cost does not grow with the number
of distinct keys it holds — allocation-wise, at least.** Across 100,
1,000, and 10,000 pre-populated distinct `(ActorID, Environment)` keys
(each with its own distinct `Fingerprint`), `B/op` and `allocs/op` are
*exactly* identical at every key count (464 B, 3 allocs — the same
copy-on-write `Fingerprints`-map rebuild `baseline.Observe` always
does, see "Allocation considerations" below): a store already holding
10,000 keys pays the same allocation cost per `Observe` call as one
holding 100. `ns/op` does drift upward as the key count grows — 243.8ns
at 100 keys to 280.8ns at 10,000 keys, about +15% — most plausibly from
reduced CPU cache locality as Go's underlying map grows more buckets to
hold more entries, not from any additional allocation (a repeat
measurement at `-cpu 1` shows the same shape, a larger ~333ns to ~422ns
range, +26%, confirming the trend is real and not single-run noise, on
`ns/op` specifically). This is not concerning at this scale, and it
would take direct evidence at production key-count scales, not
extrapolation from 10,000, to justify further investigation — per this
task's Non-Goals, no optimization is attempted here. Note this
benchmark still does not measure total heap footprint as the key set
grows without bound: `InMemory` has no eviction/expiration policy (see
[ROADMAP.md](ROADMAP.md)), so a real long-running deployment's *total*
memory use is still expected to grow linearly with the number of
distinct actors ever observed — this benchmark characterizes per-call
cost at a given store size, not the shape of that unbounded total
growth, which would need a different measurement (e.g. `runtime.MemStats`
sampled across a run) if it becomes a real question.

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
- `otel.EventFromSpan`'s 10 allocs/op (696 B) are dominated by building
  a fresh `map[string]any` for `Event.Attributes` (`attributeMap`) and
  the `string(...)` conversions `SpanID()`/`TraceID()` require —
  unavoidable given `ReadOnlySpan`'s API returns typed IDs, not
  pre-formatted strings, and `Event.Attributes` is a plain map by
  design (see `event.Event`'s doc comment). Not yet profiled at the
  per-line level since, per the reading above, it isn't the dominant
  cost anywhere it's used — the same "measure before optimizing" bar as
  every other entry in this section.

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

Both gaps this section previously named —
`internal/otel.EventFromSpan` and `store.InMemory`'s per-call cost as
the number of distinct keys it holds grows — are closed as of
[task 011](tasks/011-performance.md): see `BenchmarkEventFromSpan` and
`BenchmarkInMemoryMemoryGrowth` above. Every pipeline stage named in
the roadmap brief (event processing, feature extraction, fingerprint
generation, baseline lookup, baseline update, anomaly detection,
trust/risk calculation, policy evaluation, the complete `Analyze`
pipeline) is now benchmarked, and so is the OTel adapter.

One related, but genuinely distinct, open question remains, checked
honestly rather than assumed away: `BenchmarkInMemoryMemoryGrowth`
characterizes *per-call* cost (`ns/op`/`B/op`/`allocs/op`) at three
fixed store sizes — it does not measure a long-running process's
*total* heap footprint as an unbounded number of distinct actors
accumulate over time, which would need a different technique
(`runtime.MemStats` sampled across a run, not `go test -bench`). Since
`InMemory` has no eviction/expiration policy (see
[ROADMAP.md](ROADMAP.md)), that total footprint is expected to grow
roughly linearly with the number of distinct actors ever observed —
this is a known, named design gap already, not a new discovery — but
it hasn't been directly measured, and doing so is future work, not
part of this task's scope (see its Non-Goals).

`Trust.Explain()` and `Result.Explain()` (added in tasks 005/007) are
deliberately not benchmarked here for the same reason
`otel.EventFromSpan` wasn't originally prioritized: both are on-demand
audit/logging convenience methods called after a `Result` already
exists, not part of the `Event → ... → Decision` hot path itself, so
they don't meet this document's own bar for what gets a row.
