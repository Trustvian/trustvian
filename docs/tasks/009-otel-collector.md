# 009 — OTel Collector Processor (Separate Module)

**Milestone:** v0.2 · **Depends on:** [008](008-otel.md) (needs the
outbound attribute writer to have anything useful to enrich telemetry
with); a persistent `Store` from [003](003-baseline.md) is strongly
recommended before this is genuinely production-useful, though not a
hard technical blocker · **Blocks:** none

## Objective

Design, and build a minimal working version of, the first
production-oriented OTel Collector processor — as a **separate Go
module**, per [ADR 0003](../adr/0003-opentelemetry-adapter-single-module.md),
so the `otelcol-builder` toolchain's dependency weight never touches
this repository.

## Why

This is named directly in the original spec and the roadmap brief as
one of the primary OSS deliverables, and [ADR 0003](../adr/0003-opentelemetry-adapter-single-module.md)
already committed to *how* it must be built (separate module) without
building it. This task is where that commitment is exercised for the
first time.

## Scope

- A new repository or a clearly-separated directory with its own
  `go.mod` (decide during implementation which is cleaner given
  tooling/CI constraints; a separate top-level directory with its own
  module, e.g. `processor/`, kept out of this module's build via its
  own `go.mod`, is the minimum bar — a fully separate repository is
  also acceptable and arguably cleaner for versioning independence).
- The processor: receives OTLP spans, converts each to an `Event` via
  `internal/otel.EventFromSpan` (this means the processor module
  imports `github.com/Trustvian/trustvian` — the same relationship any
  other embedder has, not a privileged one), calls
  `Engine.Analyze`+`Engine.Observe`, writes the resulting attributes
  back onto the span via [008](008-otel.md)'s
  `AttributesFromResult`.
- Configuration: at minimum, which `Policy` to run (see
  [ADR 0002](../adr/0002-public-api-boundary.md) — this processor is
  in-module-equivalent for `internal/policy` purposes since it directly
  imports this module's source... actually **no** — a separate module
  cannot import `internal/policy` at all. This is a load-bearing
  design constraint this task must resolve: the processor can only
  configure `Engine` through options that take *public* types today
  (`WithStore`, but not `WithPolicy` with a custom `Policy` — see
  [ADR 0002](../adr/0002-public-api-boundary.md)). Decide during
  implementation whether this task is what finally justifies promoting
  `Policy`/`Config` to a public package (a real external consumer,
  arguably the first one, now exists) — if so, that's itself the
  concrete trigger [ADR 0002](../adr/0002-public-api-boundary.md)
  named for revisiting the decision, and should be scoped explicitly,
  not done silently as a side effect of this task.
- An example Collector deployment config (`config.yaml`) demonstrating
  the processor in a pipeline.

## Non-Goals

- No distributed/multi-instance Trustvian server — one processor
  instance, one in-process (or file-backed, per 003) `Store`, matching
  the roadmap's explicit "do not build a distributed Trustvian server
  yet."
- No Kubernetes/Helm packaging for the processor — a working binary
  and a documented Collector config are the bar; deployment packaging
  is separately-justified future work.
- No new policy language or dynamic policy reload — whatever policy
  configurability this task adds is bounded by
  [006](006-policy.md)'s existing scope, not an excuse to build more.

## Technical Requirements

- Must build and run with **no dependency edge back into this
  module's internal packages** — only the public API (`trustvian`,
  `event`) plus whatever [ADR 0002](../adr/0002-public-api-boundary.md)
  reconsideration (above) makes public.
- Concurrent-safe: the Collector runs processors on a shared pipeline;
  `Engine`'s existing thread-safety (documented, benchmarked under
  `-race`) must hold under the processor's actual call pattern —
  verify, don't assume, since this is a new caller shape (concurrent
  spans from a Collector's batch processing, not a single CLI process).
- Observable: expose at minimum a count of processed spans and
  decisions-by-type — the concrete "observable" requirement from the
  roadmap brief — via whatever mechanism is idiomatic for a Collector
  component (its own internal telemetry conventions).

## Tests

- Unit tests for the processor's span → `Event` → `Result` → enriched
  span pipeline, using the same span-construction technique
  `internal/otel/otel_test.go` already established (a real
  `TracerProvider` + capturing exporter — `sdktrace.ReadOnlySpan`
  cannot be faked).
- A concurrency test: many goroutines processing spans through one
  processor instance concurrently, run under `-race`.

## Benchmarks

- End-to-end processor throughput (spans/sec) under realistic span
  volume — the first genuinely new hot-path in this roadmap, since a
  Collector component's whole job is per-span overhead at scale.

## Documentation

- New `README.md` in the processor module/repository (separate from
  this repository's own).
- [OPENTELEMETRY.md § the OTel Collector processor](../OPENTELEMETRY.md#the-otel-collector-processor-planned-separate-module):
  update from "planned" to point at the real location once it exists.
- If [ADR 0002](../adr/0002-public-api-boundary.md) is revisited as
  part of this task, a new ADR recording that decision.

## Acceptance Criteria

- The processor module builds independently, with its own `go.mod`,
  and this repository's `go.mod`/`go.sum` are completely unaffected by
  its existence (verify: `git diff go.mod go.sum` in *this* repo is
  empty after the processor module is created elsewhere).
- A documented example Collector pipeline config runs end-to-end
  against a real (or realistic test) OTLP source and produces enriched
  spans with `trustvian.*` attributes.
- Concurrency tests pass under `-race`.
- Whether [ADR 0002](../adr/0002-public-api-boundary.md) was revisited
  is an explicit, documented decision either way — not silently
  worked around.
