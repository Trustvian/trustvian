# 011 — Performance Baseline Completion

**Milestone:** v0.1 · **Depends on:** none · **Blocks:**
[013](013-oss-v01.md)

## Objective

Close the two gaps [PERFORMANCE.md](../PERFORMANCE.md) already
explicitly names as unmeasured — an OTel-adapter benchmark and a
sustained-memory-growth test — rather than build a new performance
effort from scratch. Every pipeline stage named in the roadmap brief
(event processing, feature extraction, fingerprint generation,
baseline lookup, baseline update, anomaly detection, trust/risk
calculation, policy evaluation, the complete `Analyze` pipeline) is
**already benchmarked** — see the full table in
[PERFORMANCE.md](../PERFORMANCE.md).

## Why

"Do not publish performance claims until benchmarks are reproducible"
— true today for everything already measured (every number in
`PERFORMANCE.md` states its exact environment and reproduction
command). The two gaps are named explicitly in that same document's
own "What's not benchmarked (yet)" section, written during the prior
architecture-hardening pass specifically so they wouldn't be silently
forgotten.

## Scope

1. **`internal/otel.EventFromSpan` benchmark.** Using the same
   real-span construction technique `internal/otel_test.go` already
   uses (a `TracerProvider` + capturing `SpanExporter` — `ReadOnlySpan`
   can't be faked), benchmark span-to-`Event` conversion for a
   representative span shape.
2. **`store.InMemory` sustained memory growth.** A benchmark or test
   (decide which is more appropriate — likely a benchmark with
   `-benchmem` reporting steady-state `B/op` as actor/fingerprint count
   grows) simulating an unbounded number of distinct `(ActorID,
   Environment)` keys and fingerprints over many observations, to
   characterize (not necessarily fix — see Non-Goals) memory growth
   shape.
3. Once [004](004-anomaly.md) and [006](006-policy.md) land, re-run
   the full suite and update [PERFORMANCE.md](../PERFORMANCE.md)'s
   table with current numbers — every code change in v0.1 that touches
   a benchmarked path should be reflected here before the release gate
   ([013](013-oss-v01.md)).

## Non-Goals

- No optimization work driven by this task alone — if the
  memory-growth characterization reveals a real problem, that becomes
  its own, separately-justified task (with its own before/after
  numbers), not silently folded into this one. This task's job is
  *measuring*, per the roadmap principle "performance before scale" —
  understanding the current shape before deciding whether scale work
  is even needed.
- No new benchmarking framework/tooling — `go test -bench` +
  `-benchmem`, exactly as every existing benchmark already uses.

## Technical Requirements

- Both new benchmarks follow the established house style: `b.Loop()`,
  `b.ReportAllocs()`, common-case vs. worst-case split where
  applicable (see [`.claude/rules/testing.md`](../../.claude/rules/testing.md)).

## Tests

Not applicable in the traditional sense — this task's deliverable is
benchmarks, not correctness tests (those already exist for both
subjects).

## Benchmarks

- `BenchmarkEventFromSpan` (`internal/otel`).
- `BenchmarkInMemoryMemoryGrowth` or equivalent (`internal/store`) —
  reports `B/op`/allocs at increasing key counts (e.g. 100, 1,000,
  10,000 distinct keys) to show the growth curve, not just a single
  data point.

## Documentation

- [PERFORMANCE.md](../PERFORMANCE.md): add both new results to the
  measured-results table and "what's not benchmarked" section (removing
  the two closed items, adding whatever the next real gap turns out to
  be, if any).

## Acceptance Criteria

- Both new benchmarks exist, run, and their numbers are recorded with
  the same environment-disclosure rigor as every existing entry
  (Go version, OS/arch, CPU model).
- [PERFORMANCE.md](../PERFORMANCE.md)'s "what's not benchmarked" list
  no longer includes these two items.
- If the memory-growth characterization reveals non-linear or
  otherwise concerning growth, that finding is documented (as a fact,
  with numbers) even if no fix is attempted in this task.
