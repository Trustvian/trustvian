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

`Config` has one field, `policy`, declaring a real Trustvian `Policy`
in exactly the same schema the Go SDK
(`config.LoadFile`/`config.CompilePolicy`) and the CLI
(`trustvian analyze --config`) already consume — see the core
repository's [Policy Guide](../docs/policy-guide.md) for the field
reference. `WithAnomalyConfig`, `WithTrustConfig`, `WithStore`, and
`WithContextRisk` remain unconfigurable from here for the same reason
they always were (see below): every other Trustvian `Option` still
takes a type from the core module's `internal/` packages that this
module — a genuinely separate one — structurally cannot construct.

```yaml
processors:
  trustvian:
    policy:
      version: v1
      default_decision: observe_only
      default_reason: no policy rules configured; observing by default
      rules:
        - name: block-critical-risk
          when:
            min_risk_level: critical
          decision: block
          reason: critical risk is blocked by configured policy
```

**Omitting `policy:` entirely** preserves this processor's original
behavior exactly: every span still resolves to
`trustvian.decision = "observe_only"`, `NewEngine()`'s
zero-configuration default. The `trustvian.*` score/risk/fingerprint
attributes are always genuinely computed and meaningful regardless.

**An invalid or incomplete explicit `policy:` block** (an unrecognized
schema version, a missing `default_decision`/`default_reason`, an
invalid decision or condition value, or a duplicate rule name) fails
the whole Collector's startup — `CreateTraces` returns an error before
any pipeline runs, never a fallback to the default policy at the first
span.

Why `Config.Policy` is typed as a generic map (`map[string]any`)
rather than `config.PolicyConfig` directly: see the core repository's
[task 022](../docs/tasks/022-collector-config-integration.md) for the
full reasoning — in short, Collector's own confmap decoder only reads
`mapstructure` struct tags, matched case-sensitively, and
`config.PolicyConfig` only carries the `yaml:"..."` tags its own file
loader (task 020) added; `decodePolicy` (`config.go`) bridges that gap
by decoding with `go-viper/mapstructure/v2` pointed directly at those
existing `yaml` tags, producing a real `config.PolicyConfig` with zero
duplicate policy model anywhere in this module.

**Not yet resolvable outside a local workspace:** `processor/go.mod`
still requires `github.com/Trustvian/trustvian v0.3.0`, a version that
predates the `config` package. This processor's policy-configuration
code is implemented and tested (see `go.work` at the repository root,
git-ignored, for local development), but `processor/go.mod`'s
committed dependency cannot yet point at a released version that
contains `config` — no Trustvian core release newer than `v0.4.0` has
been pushed to `origin` as of this writing. See task 022's own
"Release / Module Compatibility" section for the exact verification and
the release this depends on.

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
a malformed-span-doesn't-fail-the-batch test, a concurrency test (many
goroutines calling `ConsumeTraces` on one processor instance), and —
since task 022 — the `policy:` configuration path: decoding a real
`policy:` block through Collector's own `confmap` decoder, an invalid
policy failing `CreateTraces` outright, a configured Policy actually
changing a real span's `trustvian.decision`, an omitted `policy:`
preserving the pre-task default, and first-match-wins rule ordering
surviving decode + compile.

## Non-goals (this version)

No distributed/multi-instance Trustvian server. No Kubernetes/Helm
packaging. No new policy language, matchable condition, or dynamic
policy reload (the configured Policy compiles once, at processor
creation, and is fixed for the processor's lifetime). No exported
Collector-convention metrics for the observability counters above. No
Alert configuration (`alerts:`/`sinks:`/`webhook:` in Collector
config) — a separate, future task. These match the scope boundaries in
the core repository's
[`docs/tasks/009-otel-collector.md`](../docs/tasks/009-otel-collector.md)
and [`docs/tasks/022-collector-config-integration.md`](../docs/tasks/022-collector-config-integration.md).
