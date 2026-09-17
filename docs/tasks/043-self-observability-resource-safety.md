# 043 — Self-Observability & Resource Safety

**Milestone:** v0.9 — Operational Readiness · **Depends on:**
[042](042-runtime-health-readiness-graceful-shutdown.md) (the lifecycle
state this observes) · **Blocks:** 045 (`v0.9` stabilization) · **Fifth
slice of `v0.9`.**

## Objective

Make Trustvian operationally observable without feeding its own telemetry
back into the behavioral-analysis engine, and audit and enforce bounded
resource usage for the long-lived runtime.

This is about the health of Trustvian itself. It is not about observing
actors more deeply.

## The two streams must not meet

```text
actors' behavior  →  Trustvian engine  →  decisions        (the product)

Trustvian runtime →  operational metrics → OTel pipeline    (this task)
```

Operational metrics describe *Trustvian*; behavioral telemetry describes
*its subjects*. Routing the first into the second would create a feedback
loop where the engine analyzes its own analysis, producing anomaly signals
about nothing. Nothing in this task connects them, and nothing in the
default configuration can.

## Existing telemetry audit

| Signal | Existing owner | Reusable | Gap |
|---|---|---|---|
| Traces (inbound) | `internal/otel` — maps spans to Events | Yes, unchanged | none |
| Traces (outbound) | processor — writes `trustvian.*` span attributes | Yes, unchanged | none |
| Logs | processor — `zap`, via Collector `TelemetrySettings` | Yes, unchanged | none |
| Metrics | **none** | — | **everything below** |
| Health | task 042 — `/livez`, `/readyz` | Yes, authoritative | none |
| Process/runtime metrics | Collector (memory, CPU, uptime) | Yes — not duplicated | none |
| Pipeline metrics | Collector (`otelcol_*` accepted/refused/dropped spans) | Yes — not duplicated | none |

### What not to duplicate

The Collector already counts spans accepted, refused, and dropped per
component, and already reports process memory, CPU, and uptime. Emitting a
Trustvian span counter would produce a second, subtly different number for
the same thing, and operators would have to learn which one to trust.

So this task instruments **only what Trustvian uniquely knows**: what its
engine decided, how long its own analysis took, and whether its learning
path succeeded. None of that is visible to the Collector.

## Where instrumentation lives

**In the processor, not the engine.**

The root module's core — `event` through `internal/policy` and the root
`Engine` — has zero OpenTelemetry in its dependency graph, and
`.claude/rules/go.md` confines OTel to `internal/otel`. Instrumenting
`Engine.Analyze` would pull an OTel dependency into the engine that every
SDK consumer would then carry, to serve a runtime concern that only the
long-lived service has.

The processor already has OTel, already receives a `MeterProvider` through
the Collector's `TelemetrySettings`, and already tracks exactly these facts
in its in-memory `Stats`. Instrumenting there:

- adds no dependency to the core,
- needs no global meter state — the provider is injected, which is what
  keeps it testable,
- and leaves provider lifecycle with the Collector, which owns it. Trustvian
  must not shut down a provider it did not create; doing so would
  double-shutdown the Collector's own telemetry.

## Metrics

Five instruments. Names follow OpenTelemetry conventions: namespaced,
lowercase, no `total`/`count` suffix on counters (exporters add those), UCUM
units, and durations in seconds.

| Metric | Type | Unit | Attributes | Cardinality |
|---|---|---|---|---|
| `trustvian.analyses` | Counter | `{analysis}` | `trustvian.outcome` | **3** |
| `trustvian.decisions` | Counter | `{decision}` | `trustvian.decision` | **7** |
| `trustvian.analysis.duration` | Histogram | `s` | *(none)* | **1** |
| `trustvian.observations` | Counter | `{observation}` | `trustvian.outcome` | **3** |
| `trustvian.observe.duration` | Histogram | `s` | *(none)* | **1** |

Total time series: **15**, fixed, regardless of how many actors, events, or
environments the deployment sees. That property is the point.

> Corrected during implementation: this table first said 6 decision series
> and 14 total, which contradicted the same section's own requirement that
> an unrecognized decision record as `other`. `other` is a seventh
> series — that is what makes the bound hold — so the total is 15. The
> design did not change; the arithmetic was wrong.

### Attribute vocabularies

Every attribute has a closed vocabulary, enumerated in code as constants —
not derived from input:

- `trustvian.outcome` on `trustvian.analyses`: `analyzed`, `invalid_event`,
  `error`. Three, because that is every way a span can end in this
  processor.
- `trustvian.decision`: the six `policy.Decision` values — `allow`,
  `observe_only`, `alert`, `challenge`, `require_approval`, `block`. Derived
  from the domain, not invented. A value outside the set is recorded as
  `other`, so a future decision type cannot silently expand cardinality
  before anyone updates this list.
- `trustvian.outcome` on `trustvian.observations`: `learned`,
  `not_eligible`, `error`. The learning-eligibility gate's own outcomes.

### What is forbidden as an attribute

Actor IDs, session IDs, trace IDs, span IDs, target names, operation names,
fingerprints, environments, tenant identifiers, raw error strings, DSNs,
database hostnames, prompts, tool arguments, and request bodies.

Every one of those is either unbounded or is data about a *subject* rather
than about Trustvian. Metrics describe the runtime; the behavioral detail
already travels on the span, where it belongs and where sampling applies.

Error *categories* are recorded, never error text: a raw `error.Error()` as
a label is the classic way a metrics backend acquires an actor identifier
by accident.

### Why no health gauge

Task 042's `/readyz` is the authoritative health surface, and a
`ready=0/1` gauge would be a second answer to the same question that can
disagree with the first during a scrape gap. The probe is cheap, live, and
already documented. Not added.

## Behavior when telemetry is absent or failing

Instrumentation is a side effect and never a dependency:

- **No `MeterProvider`** — the Collector always supplies one, but if it is a
  no-op, every instrument is a no-op. Nothing branches on it.
- **Export failure** — owned entirely by the OTel SDK and the Collector's
  exporter pipeline. Recording a measurement does not block on export, so a
  broken metrics backend cannot slow or alter a decision.
- **No synchronous remote call is added to the hot path.** Recording is an
  in-process atomic update; export is asynchronous and belongs to the SDK.
- **No custom sampling.** Aggregation and export cadence are the SDK's and
  the operator's to configure.
- **Instrument construction failure degrades, it does not fail startup.**
  Names are constants, so a conformant SDK cannot reject them — but a
  non-conformant one must not be able to stop a security component from
  deciding. The processor logs and runs uninstrumented; `Metrics`'s nil
  and zero values record nothing safely, which tests assert.

A security decision must never depend on an observability backend, and
after this task it still does not.

## Resource-safety audit

The audit's most useful result is how little there is. Non-test Trustvian
code contains **one** goroutine, **zero** channels, **zero** tickers, and
**zero** timers.

| Resource | Owner | Bound | Shutdown owner | Finding |
|---|---|---|---|---|
| Health-server goroutine | processor (task 042) | one, serves until closed | `Shutdown` → `http.Server.Shutdown` | Sound; no change |
| Channels / queues | — | none exist | — | Nothing to bound |
| Tickers / timers | — | none exist | — | Nothing to leak |
| PostgreSQL pool | pgx | `MaxConns`, lifetime 1h, idle 30m | `Shutdown` → `Close`, exactly once | Inherently bounded; no change |
| InMemory store | `store.InMemory` | O(distinct actors), each capped | process lifetime | Documented below |
| Meter / instruments | Collector's `MeterProvider` | fixed, 15 series | **Collector** — not Trustvian | Must not be shut down here |
| Health model | processor | one struct | `Shutdown` | Sound |
| Engine | processor | one, stateless per call | process lifetime | Sound |

No unbounded Trustvian-owned queue exists, and none is introduced. No
global concurrency limiter is added: the Collector owns pipeline
concurrency, and adding a second limiter would create two control layers
fighting over the same throughput.

### PostgreSQL pool

Trustvian sets only `MaxConns`, and only when configured; everything else
is pgx's default — `MinConns` 0, `MaxConnLifetime` 1h, `MaxConnIdleTime`
30m, `HealthCheckPeriod` 1m. All finite. The pool cannot grow without
bound, and connections are recycled rather than held forever.

**No new configuration is added.** pgx already accepts `pool_max_conns`,
`pool_max_conn_lifetime`, and the rest as DSN parameters, so an operator
who needs them already has them. Duplicating pgx's configuration surface in
Trustvian's would create two places to set one value.

### InMemory growth

`InMemory` holds `map[baseline.Key]*shard`, one entry per distinct
`{ActorID, Environment}`. There is **no TTL, no eviction, and no maximum**,
and this task does not add any.

That is intentional, not an oversight: a behavioral baseline exists to
accumulate what normal looks like for an actor, so evicting it would
silently reset that actor to "never seen" and make the next legitimate
action look novel. Adding eviction would change behavioral semantics, which
§27 of this task's brief and CLAUDE.md both rule out.

The growth is bounded *per actor* — task 036 measured a baseline driven
past every cardinality cap at ~13 KB — so total memory is approximately
`13 KB × distinct actors`: roughly 13 MB at 1,000 actors, 130 MB at 10,000.
Operators running long-lived deployments with many actors should use
PostgreSQL, which is what it is for. This is documented as an operational
characteristic rather than hidden.

## Configuration

**None added.** Instrumentation activates automatically from the
`MeterProvider` the Collector already supplies. Every existing Trustvian and
Collector configuration keeps working unchanged, and no operator has to
configure a metrics backend inside Trustvian — that is the Collector's
exporter configuration, where it belongs and stays vendor-neutral.

## Tests

- Instruments created, incremented, and recorded, using OTel's in-memory
  metric reader — no live backend.
- Decision attribute proven to be one of the actual `policy.Decision`
  values, so a cardinality regression fails a test rather than a dashboard.
- Attribute sets asserted to contain **no** actor, trace, or error-text
  values — checked against a recorded metric, not against documentation.
- Errors forced; error counter increments with a bounded category and no
  raw error text.
- Concurrent analysis under `-race`, verifying no race and sane
  aggregation.
- Existing benchmarks re-run to show instrumentation overhead is not
  material on the analysis path.

## What implementation changed

Three things the frozen spec above did not anticipate, each recorded
rather than quietly absorbed:

**1. Cardinality is 15, not 14.** Corrected in place above — `other` is a
seventh decision series, which is what makes the bound hold.

**2. Duration histograms needed explicit bucket boundaries.** The spec
said "durations in seconds" and stopped there. A live Collector scrape
showed every measurement landing in the first bucket, because the SDK's
default boundaries (`0, 5, 10, … 10000`) assume milliseconds. Both
histograms now carry explicit advice spanning 1 ms to 10 s. Counting was
never wrong; the distribution was unreadable.

**3. Attribute sets are pre-built, not assembled per call.** The first
implementation called `metric.WithAttributes` at each recording site,
which constructs an `attribute.Set` — sort, dedup, allocate — on every
span, *including when the meter is a no-op*. Benchmarks showed 3 extra
allocations per span against the pre-task baseline. Pre-building one
option per vocabulary entry at construction removed them entirely, and
made the cardinality bound structural: a value with no pre-built option
cannot be recorded at all.

| Configuration | ns/op | B/op | allocs/op |
|---|---|---|---|
| Pre-task baseline (HEAD, no metrics) | 1210 | 1352 | 33 |
| Instrumented, no-op meter | 1226 | 1352 | 33 |
| Instrumented, real metrics SDK | 1562 | 1353 | 33 |

The residual ~350 ns is the SDK's own aggregation across five
measurements, paid only when a metrics pipeline is configured.

## Findings outside the metrics themselves

**The bundled demo collector cannot export these metrics.**
`cmd/trustvian-collector` hand-builds a telemetry factory with a no-op
`MeterProvider`, to keep the demo's dependency graph free of the cloud
resource detectors and Kubernetes client that `otelconftelemetry` pulls
in. So the binary the reference deployment runs records these metrics and
exports none of them, and rejects `service.telemetry.metrics` for the
same reason.

Not fixed here, deliberately: the alternatives are adding a vendor
metrics exporter (an explicit non-goal) or adopting `otelconftelemetry`'s
dependency weight in a binary whose stated purpose is minimality. Both
are larger decisions than this task's scope. Documented in
[`observability.md`](../observability.md) and `processor/README.md`
instead, with the `ocb` path that works.

**A task 042 test sampled a flag on the wrong side of an HTTP round
trip.** `TestShutdownTransitionsReadinessBeforeClosingStore` read
`shutdownBegun` *after* its probe returned, so a 200 served legitimately
before shutdown was attributed to after it — observed once as a spurious
failure under heavy machine load. The prober now samples before issuing,
and the flag is set by the shutdown goroutine itself. Production code
unchanged; the defect was entirely in the test.

**A task 042 changelog entry was filed under the released `v0.8.0`
section**, though its commit postdates the `v0.8.0` tag. Moved to
`Unreleased`.

## Non-Goals

No Prometheus client, StatsD, Datadog SDK, or any vendor dependency. No
Prometheus server, Grafana, dashboards, or dashboard JSON. No second
telemetry backend and no custom metrics protocol. No metric sampling logic.
No new detector, storage backend, or behavioral capability. No backup or
restore (044). No Kubernetes or Helm. No second shutdown coordinator, and no
shutdown of the Collector's `MeterProvider`.

## Acceptance Criteria

1. Collector-native telemetry audited; no generic Collector metric
   duplicated.
2. OpenTelemetry Metrics used, with no vendor dependency added.
3. Analysis count, decision count, analysis latency, and operational error
   count available; store-path outcomes observable through the observe
   instruments.
4. Every metric documents type, unit, attributes, and cardinality bound.
5. Every attribute has a closed, enumerated vocabulary.
6. No actor IDs, trace IDs, raw errors, credentials, DSNs, or behavioral
   payload in any attribute — enforced by test.
7. Operational metrics do not feed behavioral analysis, and cannot by
   default.
8. Telemetry failure cannot alter a decision; no synchronous remote call on
   the hot path.
9. OTel provider lifecycle stays with the Collector.
10. Goroutines, channels, timers, InMemory growth, pool bounds, and I/O
    timeouts all audited and documented.
11. Every newly introduced resource has explicit shutdown ownership; no
    unbounded queue and no unnecessary global limiter added.
12. Metrics, cardinality, and race tests pass.
13. All prior gates pass: root, module consistency, processor `GOWORK=off`,
    examples, PostgreSQL, task 042 health behavior, release dry-run,
    container build, and the vulnerability gates unchanged.
14. No configuration change required of existing deployments.
15. Documentation synchronized; `README.md` remains evergreen.

## Verification

Gates, all with `GOWORK=off` so a workspace cannot mask a module
boundary:

| Gate | Result |
|---|---|
| `gofmt -l` (all modules) | clean |
| Root: build, vet, test, `-race` | pass (15 packages) |
| Processor: `go mod verify`, build, vet, test, `-race` | pass |
| Examples: build, vet | pass |
| `./scripts/check-modules.sh` | OK |
| `govulncheck` — root / processor / examples | 0 / 0 reachable / 0 |
| PostgreSQL integration (root store + processor) | pass |
| Task 042 health regression | pass, incl. 20× repeat of the corrected test |

End-to-end, against a real Collector built with `otelconftelemetry`, a
real PostgreSQL store, and a Prometheus pull reader — 60 spans from
`demo-producer`:

```text
trustvian_analyses{trustvian_outcome="analyzed"} 60
trustvian_decisions{trustvian_decision="allow"} 60
trustvian_observations{trustvian_outcome="learned"} 60
trustvian_analysis_duration_count 60     # 59 under 1 ms, 1 cold-start outlier at 5-7.5 ms
trustvian_observe_duration_count 60      # 16 under 1 ms, 57 under 2.5 ms
```

`/livez` and `/readyz` both returned `{"status":"ok"}` throughout.

Gates were also proved load-bearing by mutation rather than assumed:
folding of unknown decisions disabled → cardinality and privacy tests
fail; a forbidden attribute (actor ID, raw error text) added at a call
site → privacy test fails naming both; `RecordAnalysis` removed from
`processSpan` → three wiring tests fail.
