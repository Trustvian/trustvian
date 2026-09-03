# OpenTelemetry Adapter

`internal/otel` is the *only* package in this module that imports
`go.opentelemetry.io/otel*`. The core engine (`event` through
`internal/policy`, and `Engine` itself) has zero OpenTelemetry
dependency — see
[Architecture § package boundaries](ARCHITECTURE.md#package-boundaries).

It's currently `internal/`, so — like `Policy` and the `Config` types —
it's usable by code inside this repository (this is where a future
OpenTelemetry Collector processor, itself a separate module/deliverable,
would use it) but not yet importable from a separate Go module. See
[Go SDK Guide § the public/internal boundary today](sdk-guide.md#the-publicinternal-boundary-today).

## What it does

```go
func EventFromSpan(span sdktrace.ReadOnlySpan) event.Event
```

A pure, deterministic mapping from one finished OpenTelemetry span to
one `event.Event`. It uses standard semantic conventions where they
exist, and four documented `trustvian.*` attributes as an escape hatch
for what no convention covers yet (there is currently no standard
convention for, e.g., "this span represents an AI-agent tool call").

## Mapping table

| `Event` field | Derived from |
|---|---|
| `ID`, `Context.SpanID` | `span.SpanContext().SpanID()` |
| `Context.TraceID` | `span.SpanContext().TraceID()` |
| `Timestamp` | `span.StartTime()` |
| `Operation.Name` | `span.Name()` |
| `Operation.Category` | `http.request.method` → `http`; `db.system.name` → `db`; `rpc.system.name` → `rpc`; else falls back to `rpc` (see below) |
| `Operation.Direction` | `span.SpanKind()`: `Server`/`Consumer` → `inbound`, `Client`/`Producer` → `outbound`, else unspecified |
| `Target.Name` | `service.peer.name`, then `db.namespace`, then `server.address` — most specific first |
| `Actor.ID` | resource `service.name` |
| `Actor.Type` | defaults to `service` |
| `Actor.IdentityConfidence` | defaults to `1.0` |
| `Context.Environment` | resource `deployment.environment.name` |
| `Attributes` | every span attribute, unmapped or not — nothing is dropped |
| `Attributes["duration_ms"]` | `span.EndTime().Sub(span.StartTime())`, only if positive |
| `Attributes["error"]` | `true` if `span.Status().Code == codes.Error` |

**Why the RPC fallback.** A span matching none of HTTP/DB/RPC's
semantic conventions falls back to `Operation.Category = "rpc"` — the
most generic "some kind of call happened" bucket among Trustvian's
five categories (`http`, `db`, `rpc`, `tool`, `external`). See
[`internal/otel/otel.go`](../internal/otel/otel.go)'s `inferCategory`.

**Why latency/error are bridged, not mapped.** Span duration and
status aren't OTel *attributes* — they're separate fields on the span
itself. `features.Extract` only ever reads `Event.Attributes`, so the
adapter explicitly writes them into the two keys it understands
(`duration_ms`, `error`) rather than leaving them to be inferred.
Without this bridging, an OTel-derived event would silently carry no
latency/error signal at all.

## Trustvian-specific override attributes

| Attribute | Overrides |
|---|---|
| `trustvian.actor.id` | `Actor.ID` |
| `trustvian.actor.type` | `Actor.Type` (e.g. `"ai_agent"`) |
| `trustvian.identity.confidence` | `Actor.IdentityConfidence` |
| `trustvian.operation.category` | `Operation.Category` (e.g. `"tool"`) |

Example: an AI-agent tool-call span with no applicable standard
convention.

```go
tracer.Start(ctx, "search_customer",
	trace.WithSpanKind(trace.SpanKindInternal),
	trace.WithAttributes(
		attribute.String("trustvian.actor.type", "ai_agent"),
		attribute.String("trustvian.actor.id", "support-agent"),
		attribute.Float64("trustvian.identity.confidence", 0.9),
		attribute.String("trustvian.operation.category", "tool"),
	),
)
```

maps to an `Event` with `Actor.Type = ActorTypeAIAgent`,
`Actor.ID = "support-agent"`, `Actor.IdentityConfidence = 0.9`,
`Operation.Category = OperationCategoryTool` — exactly the shape the
[Use Cases § AI-agent security](use-cases.md#ai-agent-security) example
constructs directly as JSON, just sourced from a live span instead.

## Trustvian output attributes (not yet implemented)

The project spec names six `trustvian.*` attributes meant to *enrich*
telemetry with Trustvian's verdict, for export back into the
observability pipeline:

| Attribute | Meaning |
|---|---|
| `trustvian.anomaly.score` | `Anomaly.Score` |
| `trustvian.trust.score` | `Trust.Score` |
| `trustvian.risk.level` | `Trust.Risk` |
| `trustvian.decision` | `Result.Decision` |
| `trustvian.fingerprint.id` | `Fingerprint.ID` |
| `trustvian.behavior.id` | reserved; not yet defined — see below |

**These are not implemented anywhere in this repository today.**
`internal/otel.EventFromSpan` is inbound-only (span → `Event`); nothing
writes attributes back onto a span or exports them. Do not confuse
these *output* attributes with the four *input* override attributes
above (`trustvian.actor.id`, etc.) — the two serve opposite directions
of the same adapter boundary, and only the input direction exists so
far. `trustvian.behavior.id` in particular has no defined meaning yet
in this codebase (it's carried over from the spec's original naming
and hasn't been reconciled against `Fingerprint.ID`, which may be all
that's needed). See [ROADMAP.md](ROADMAP.md) (`v0.2`) and
[`tasks/008-otel.md`](tasks/008-otel.md) for the scoped implementation
task, naturally paired with the Collector processor below since a
Collector processor is the most likely place enrichment actually
happens (enriching a trace as it passes through, rather than the core
engine reaching back into telemetry it doesn't own).

## The OTel Collector processor (planned, separate module)

The spec's Phase 2 also calls for an OTel Collector processor — a
deployable Collector component that scores telemetry in-flight and
(once the output attributes above exist) enriches it. This is
intentionally **not** part of this module:

- It depends on the `otelcol-builder` toolchain, a materially heavier
  dependency tree than the lightweight OTel API/SDK packages
  `internal/otel` uses.
- Building it means importing this module's public API (`Engine`) plus
  `internal/otel`'s mapping (or a similar one), from a separate
  repository/module — the same shape as any other embedder, not a
  privileged internal dependency.

See [ADR 0003](adr/0003-opentelemetry-adapter-single-module.md) for
the full reasoning, [ROADMAP.md](ROADMAP.md) (`v0.2`) for status, and
[`tasks/009-otel-collector.md`](tasks/009-otel-collector.md) for the
scoped implementation task — including the open question of whether
building this is what finally justifies revisiting
[ADR 0002](adr/0002-public-api-boundary.md)'s public API boundary.

## Best-effort, not validated

`EventFromSpan` never fabricates data it doesn't have. A span with no
resource and no override attribute produces an `Event` with an empty
`Actor.ID` — which `Event.Validate()` (and therefore `Engine.Analyze`)
correctly rejects, rather than the adapter inventing a placeholder
identity. Always check the error from `Analyze`, or call
`ev.Validate()` yourself first if you're doing something other than
immediately analyzing the result.

## Testing note for contributors

`sdktrace.ReadOnlySpan` has a deliberately unexported method — only the
real SDK can produce one, so `internal/otel`'s tests build actual spans
through a `TracerProvider` with a capturing `SpanExporter`, not a
hand-rolled fake. See
[`internal/otel/otel_test.go`](../internal/otel/otel_test.go). One
non-obvious gotcha discovered while writing those tests: a
`TracerProvider` constructed with no `WithResource` still attaches its
own default `Resource` (including a fallback `service.name`) — pass
`resource.Empty()` explicitly to test the "no resource at all" case.
