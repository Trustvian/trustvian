# trustvian-processor

An OpenTelemetry Collector processor that scores every span passing
through a Collector pipeline with [Trustvian](https://github.com/Trustvian/trustvian)
and enriches it with the outbound `trustvian.*` attributes, before
forwarding it unchanged in shape to the next consumer in the pipeline.

This is a **separate Go module**, deliberately — see
[ADR 0003](../docs/adr/0003-opentelemetry-adapter-single-module.md) in
the core repository. Building a real Collector component requires the
`go.opentelemetry.io/collector/*` component APIs, a materially heavier
dependency tree than the core engine's own lightweight OTel API/SDK
usage; keeping it in its own module means that tree never touches
`github.com/Trustvian/trustvian`'s own `go.mod`, even indirectly.

## How it fits together

```
Application (OTel SDK)
        │  OTLP
        ▼
  OTLP receiver
        │  ptrace.Traces
        ▼
trustvian processor  ← this module
        │  ptrace.Traces, now carrying trustvian.* attributes
        ▼
   (your exporter)
```

Internally, for each span:

```
ptrace.Span + resource attributes
        │  mapping.go: EventFromSpan
        ▼
     event.Event
        │  Engine.Analyze (github.com/Trustvian/trustvian, public API)
        ▼
       Result
        │  attributes.go: SetAttributesFromResult
        ▼
ptrace.Span, enriched in place
```

## Why this can't just import `internal/otel`

The core module's `internal/otel.EventFromSpan` and
`AttributesFromResult` ([task 008](../docs/tasks/008-otel.md)) cannot be
reused here, for two independent reasons:

1. **They're under `internal/`.** Go's `internal/` visibility rule
   blocks any package outside `github.com/Trustvian/trustvian` itself
   from importing them — this module is a genuinely separate module,
   so it's on the wrong side of that boundary, the same as any other
   embedder (see
   [ADR 0002](../docs/adr/0002-public-api-boundary.md)).
2. **Even without that restriction, the span type is wrong.**
   `internal/otel.EventFromSpan` takes `sdktrace.ReadOnlySpan` — a type
   only the OpenTelemetry **SDK**'s own in-process span-export path
   produces (it has a deliberately unexported method; see the core
   module's own testing notes in `docs/OPENTELEMETRY.md`). A Collector
   processor never sees that type. It receives `ptrace.Span` — the
   OTLP/pipeline data model (`go.opentelemetry.io/collector/pdata/ptrace`)
   — which shares no relationship with `sdktrace.ReadOnlySpan` at all.

So `mapping.go` and `attributes.go` in this module are **parallel
implementations**, not shortcuts around ones that could have been
shared instead. They reuse the exact same semantic-convention key
constants (`go.opentelemetry.io/otel/semconv`, a plain-constants
package with no SDK dependency) as the core module's adapter, so the
convention *names* can never drift between the two — only the
traversal code, which the two different span APIs force to differ, is
duplicated.

## Configuration

`Config` is currently empty. This processor always runs Trustvian's
**default** `Engine` configuration: the default `Policy` (which
resolves every event to `observe_only` unless you've read this far —
see below), default anomaly/trust thresholds, and an in-memory `Store`.

This is a deliberate, explicit [ADR 0002](../docs/adr/0002-public-api-boundary.md)
decision, not an oversight. Every Trustvian `Option` function
(`WithPolicy`, `WithAnomalyConfig`, `WithTrustConfig`, `WithStore`,
`WithContextRisk`) takes a type from the core module's `internal/`
packages, which this module — a genuinely separate one — structurally
cannot construct. Building this processor is not, by itself, the "real
external consumer" trigger ADR 0002 named for promoting those types to
a public package: it's built and maintained alongside the core module,
not by an independent third party with an unmet need. That promotion
remains a decision for if/when one actually appears — see the core
repository's `docs/ROADMAP.md`.

Practically, this means: **every span this processor scores gets
`trustvian.decision = "observe_only"` today**, since that's
`NewEngine()`'s zero-configuration default policy. The `trustvian.*`
score/risk/fingerprint attributes are still genuinely computed and
meaningful — only the final policy verdict is fixed until a public
configuration surface exists.

## `trustvian.behavior.id`

Deliberately not implemented — see the identical rationale in the core
module's `docs/OPENTELEMETRY.md` § Trustvian output attributes. Nothing
about running inside a Collector processor gives this attribute a
meaning it didn't already have (or rather, didn't already lack).

## Running it

This module includes a minimal, hand-assembled Collector binary
(`cmd/trustvian-collector`) that registers this processor alongside the
standard OTLP receiver and the debug exporter — no `ocb` (OpenTelemetry
Collector Builder) install required, just `go run`:

```bash
cd processor
go run ./cmd/trustvian-collector --config=config.yaml
```

Then point any OTel-SDK-instrumented application at `localhost:4317`
(OTLP/gRPC) or `localhost:4318` (OTLP/HTTP). Enriched spans are logged
to stdout by the debug exporter, e.g.:

```
Attributes:
     -> http.request.method: Str(POST)
     -> server.peer.name: Str(checkout-frontend)
     -> trustvian.anomaly.score: Double(1)
     -> trustvian.trust.score: Double(1)
     -> trustvian.risk.level: Str(low)
     -> trustvian.decision: Str(observe_only)
     -> trustvian.fingerprint.id: Str(b106dcd5d46d7ef7)
```

(captured from a real run of this exact `config.yaml` against a real
`go.opentelemetry.io/otel/sdk` span, sent over real OTLP/gRPC — not
hand-written, matching the core repository's own documentation
standard.)

The binary's own internal telemetry (its logger, tracer, meter
providers) uses a small hand-built `telemetry.Factory`
(`cmd/trustvian-collector/telemetry.go`) rather than the Collector's
full-featured default (`otelconftelemetry`), which transitively pulls
in cloud-provider resource detectors and a Kubernetes API client —
several hundred extra dependency-graph entries a "minimal working
version" (this task's own words) has no use for.

## Observability

The processor tracks (in-process only, not yet exported as Collector
metrics): total spans processed, spans that didn't map to a
`Validate()`-passing `Event`, `Engine.Analyze` errors, and a count per
`Decision` value. Exporting these as real Collector-convention metrics
is future work, not required for this minimal version.

## Testing

`go test ./... -race` covers: the `ptrace.Span` → `Event` mapping
(mirroring the core module's own semantic-convention test cases),
`Result` → attribute writing, the factory's component lifecycle, a
full `ConsumeTraces` unit test (span in, enriched span out, forwarded),
a malformed-span-doesn't-fail-the-batch test, and a concurrency test
(many goroutines calling `ConsumeTraces` on one processor instance) —
verifying the core `Engine`'s documented thread-safety actually holds
under this new caller shape, not assuming it does.

## Non-goals (this version)

No distributed/multi-instance Trustvian server. No Kubernetes/Helm
packaging. No new policy language or dynamic policy reload. No
exported Collector-convention metrics for the observability counters
above. No public configuration surface (see § Configuration). These
match the scope boundaries in the core repository's
[`docs/tasks/009-otel-collector.md`](../docs/tasks/009-otel-collector.md).
