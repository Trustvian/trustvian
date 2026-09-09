# Trustvian

**Behavioral Security & Trust Engine**

> OpenTelemetry observes behavior. Trustvian evaluates whether that
> behavior should be trusted.

Trustvian is an open-source, Go-based engine that turns runtime
behavior — API calls, service-to-service traffic, database access,
AI-agent tool calls — into an explainable trust score and a security
decision: `ALLOW`, `OBSERVE_ONLY`, `ALERT`, `CHALLENGE`,
`REQUIRE_APPROVAL`, or `BLOCK`.

Identity tells you who something is. Telemetry tells you what it did.
Trustvian determines whether what it did should be trusted.

## Status

`v0.1`–`v0.4.0` are shipped: the core pipeline, Go SDK, CLI, a
persistent file-backed store, an inbound OpenTelemetry adapter, outbound
`trustvian.*` result attributes, a standalone OTel Collector processor
([`processor/`](processor/README.md)), an opt-in hour-of-day
time-pattern anomaly signal, and a public
[`alert`](docs/DOMAIN.md#alert) package — turning a `Result`/`Decision`
into a minimal, explainable `Alert`, evaluated by a
`policy.Condition`-shaped rule matcher and delivered to a generic,
HMAC-signed HTTPS webhook, all reachable directly from the Go SDK with
no OpenTelemetry involvement (see
[`examples/alert-webhook`](examples/alert-webhook/README.md)) — are all
implemented, tested, and benchmarked.

**`v0.5` — Policy & Configuration is in progress** (its first two
tasks are done; the milestone as a whole is not): a public `config`
package now lets a caller outside this module declare a `Policy` in a
versioned YAML file — strictly validated, strictly parsed — and
compile it into a real, enforced `policy.Policy`, without ever
importing `internal/policy`. See [Configuring a Policy](#configuring-a-policy)
below.

**Trustvian OSS is meant to be a complete, standalone,
production-usable behavioral security product on its own** — detect,
score, decide, alert, integrate, and run, all without Trustvian
Control. Not yet built, on the path there: order-aware sequence
detection, delivery reliability (retry/deduplication/cooldown) and any
provider-specific alert sink (Slack/Teams/PagerDuty), declarative
*alert* configuration (`policy` configuration is now implemented — see
above), CLI/OTel Collector integration for the new policy config, AI-agent
session/delegation concepts, a production-grade persistent store beyond
`FileStore`, and release/operational engineering (CI, Docker image,
SBOM). See
[`docs/ROADMAP.md`](docs/ROADMAP.md#the-oss--enterprise-product-boundary)
for the explicit OSS/Control boundary and the full milestone sequence
through `v1.0`. ML-based detection stays optional research, never a
core dependency. See [`trustvian-project-spec.md`](trustvian-project-spec.md)
for the full long-term vision and [`CLAUDE.md`](CLAUDE.md) for the
engineering conventions this repository follows. For guides, worked
examples, and four real-world use cases with verified input/output, see
[`docs/`](docs/README.md). For what's shipped, in progress, and planned
next, see [`docs/ROADMAP.md`](docs/ROADMAP.md) and its detailed task
breakdown under [`docs/tasks/`](docs/tasks/).

## How it works

```
Event → Features → Fingerprint → Baseline → Anomaly → Trust → Policy → Decision
```

1. **Event** — one observed action: an HTTP call, a DB query, an RPC, an
   AI-agent tool invocation, or a call to an external destination.
2. **Features** — stable dimensions (actor type, operation, target,
   environment) that identify *what kind* of behavior this is, split
   from volatile ones (latency, errors) that feed anomaly detection.
3. **Fingerprint** — a deterministic identity for that behavioral
   shape.
4. **Baseline** — the statistical history for that fingerprint: how
   often it's been seen, its typical latency and error rate.
5. **Anomaly** — how much this event deviates from its baseline, with
   every contributing signal retained (never a single opaque number).
6. **Trust** — anomaly, identity confidence, and context risk combined
   into a trust score and a risk level, with each input still visible.
7. **Policy** — a data-driven, ordered rule set that turns trust/risk
   into a final `Decision`, always with a human-readable explanation.

## Install

```bash
go install github.com/Trustvian/trustvian/cmd/trustvian@latest
```

Or add the SDK to a Go project:

```bash
go get github.com/Trustvian/trustvian
```

## CLI

```bash
trustvian analyze events.json
trustvian baseline build events.json
```

`events.json` is a JSON array of events:

```json
[
  {
    "id": "evt-1",
    "timestamp": "2026-01-01T12:00:00Z",
    "actor": { "id": "svc-payment", "type": "service", "identity_confidence": 0.95 },
    "operation": { "category": "http", "name": "POST /payment" },
    "target": { "name": "payment-db" },
    "context": { "environment": "production" },
    "attributes": { "duration_ms": 42 }
  }
]
```

`analyze` scores each event and prints a report:

```
Trustvian Behavioral Analysis

Service: svc-payment
Anomaly: 1.00
Trust:   0.95
Risk:    LOW

Detected:
  ! fingerprint never observed for this actor

Decision: ALLOW
Reason:   risk within tolerance
```

`baseline build` replays a corpus of events through the same
gated learning path as live traffic and prints a learned/skipped
summary. See [Limitations](#limitations) for why its result doesn't
persist across separate CLI invocations yet.

## Go SDK

```go
package main

import (
	"context"
	"fmt"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func main() {
	engine := trustvian.NewEngine()

	result, err := engine.Analyze(context.Background(), event.Event{
		ID:        "evt-1",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "svc-payment",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryHTTP,
			Name:     "POST /payment",
		},
		Target:  event.Target{Name: "payment-db"},
		Context: event.Context{Environment: "production"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(result.Trust.Score, result.Trust.Risk, result.Decision)

	// Feed the result back in — Observe only actually learns from
	// decisions where the action proceeded (ALLOW/OBSERVE_ONLY/ALERT);
	// it's always safe to call unconditionally.
	engine.Observe(context.Background(), result)
}
```

`Engine`'s behavior (custom `Policy`, anomaly/trust thresholds, a
different `Store`) is configured via functional options
(`trustvian.WithPolicy`, `trustvian.WithAnomalyConfig`,
`trustvian.WithTrustConfig`, `trustvian.WithStore`,
`trustvian.WithContextRisk`). `WithPolicy` now has a real path for a
genuinely external caller — see [Configuring a
Policy](#configuring-a-policy) below; the other four still take types
that only code living inside this module can construct — see
[Limitations](#limitations).

For seven runnable, real `go run`-verified programs against this exact
SDK — a basic call, credential misuse, an unexpected dependency, an
external destination, abnormal request frequency, AI-agent security,
and an end-to-end alert delivered to a signed webhook — see
[`examples/`](examples/README.md).

## Configuring a Policy

By default, `NewEngine()` has no rules and always resolves to
`OBSERVE_ONLY`. A real `Policy` — one that actually produces
`ALLOW`/`BLOCK`/`REQUIRE_APPROVAL` differentiation — can now be
declared in a versioned YAML file and loaded by any caller, including
a genuinely separate Go module, without ever importing
`internal/policy`:

```yaml
# trustvian.yaml
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

```go
cfg, err := config.LoadFile("trustvian.yaml")
if err != nil {
	log.Fatal(err)
}
p, err := config.CompilePolicy(cfg)
if err != nil {
	log.Fatal(err)
}
engine := trustvian.NewEngine(trustvian.WithPolicy(p))
```

`p`'s underlying type is `policy.Policy` (an `internal/` type), but
this code never names it: `p` is received from `CompilePolicy` and
passed straight into `WithPolicy` via ordinary Go type inference — a
deliberate design, not a loophole, recorded in
[ADR 0008](docs/adr/0008-policy-config-boundary.md).

Loading is strict on purpose: an unrecognized field, an unsupported
schema version, an invalid decision/actor-type/risk-level value, or a
duplicate YAML key all fail with an actionable error rather than being
silently ignored — a config typo should never silently weaken a
policy. See [Policy Guide § Loading a Policy from a YAML
file](docs/policy-guide.md#loading-a-policy-from-a-yaml-file) for the
full field reference and [`docs/tasks/019`](docs/tasks/019-policy-config-model.md)/[`020`](docs/tasks/020-policy-config-loader.md)
for how this was built. There is no CLI `--config` flag and no OTel
Collector processor integration yet — both are separately scoped,
not-yet-started work (see [`docs/ROADMAP.md` §
v0.5](docs/ROADMAP.md#v05--policy--configuration)). Declarative *alert*
configuration (as opposed to policy configuration) does not exist
either — `alert.Rule`s are still constructed in Go only.

## OpenTelemetry

The core engine has no OpenTelemetry dependency at all. `internal/otel`
is a self-contained adapter that maps a finished span
(`sdktrace.ReadOnlySpan`) into an `event.Event` using standard semantic
conventions (HTTP, DB, RPC, `deployment.environment.name`,
`service.name`) plus four documented `trustvian.*` override attributes
for what no convention covers yet, and derives five outbound
`trustvian.*` result attributes (`AttributesFromResult`) for a caller
to attach to a span. A standalone OTel Collector processor
([`processor/`](processor/README.md), a separate Go module using the
heavier collector-builder toolchain deliberately kept out of this
module's dependency graph) consumes this SDK's public API to score
every span passing through a Collector pipeline. See
[`docs/OPENTELEMETRY.md`](docs/OPENTELEMETRY.md) for the full mapping.

## Project layout

```
trustvian/
├── trustvian.go, engine.go, options.go, result.go   # public SDK (root package)
├── event/               # public domain vocabulary: Event, Actor, Operation, Target, Context
├── alert/                # public: Result/Decision -> Alert, Evaluate, Sink, WebhookSink
├── config/               # public: versioned YAML policy config -> Validate -> CompilePolicy
├── cmd/trustvian/        # CLI
├── internal/
│   ├── features/          # Event -> stable/volatile Features
│   ├── fingerprint/       # Features -> deterministic Fingerprint
│   ├── baseline/          # statistical model (EWMA mean/variance, maturity)
│   ├── store/              # Baseline persistence port + in-memory and file-backed implementations
│   ├── anomaly/            # Features + Baseline -> Anomaly (noisy-OR combination)
│   ├── trust/               # Anomaly + identity + context -> Trust + RiskLevel
│   ├── policy/               # data-driven rule evaluation -> Decision
│   └── otel/                  # OpenTelemetry span -> Event adapter
└── trustvian-project-spec.md, CLAUDE.md   # vision and engineering conventions
```

Only the root package, `event`, `alert`, and `config` are importable
from outside this module — everything else is intentionally
`internal/`. `config` doesn't expose `internal/policy`'s types itself;
it compiles its own public config structs into them, and a caller
outside this module can still use the result via `WithPolicy` without
importing `internal/policy` — see
[ADR 0008](docs/adr/0008-policy-config-boundary.md). See
[`.claude/rules/architecture.md`](.claude/rules/architecture.md) for
the general reasoning behind this boundary.

## Limitations

- **Persistent baseline storage exists but isn't the default.**
  `store.FileStore` (a JSON file on disk, flushed synchronously after
  every `Observe`) survives a process restart; `store.InMemory` remains
  `NewEngine`'s default and does not. Switching is a one-line
  `trustvian.WithStore(...)` change — see
  [`docs/ARCHITECTURE.md` § storage boundary](docs/ARCHITECTURE.md#storage-boundary).
  The CLI's `trustvian baseline build` still only proves out its
  mechanism within a single invocation regardless of `Store`, since the
  CLI itself doesn't yet expose a flag to select `FileStore`.
- **A custom `Policy` now has a real external path; thresholds and
  `Store` don't yet.** `WithAnomalyConfig`, `WithTrustConfig`, and
  `WithStore` still take types from this module's `internal/` packages
  that a separate Go module cannot construct — a custom
  `anomaly.Config`/`trust.Config`, or a custom `store.Store`
  implementation, still requires code living inside this module.
  `WithPolicy` is the exception as of `v0.5`: the public `config`
  package (`config.LoadFile`/`Load`/`Validate`/`CompilePolicy`) lets a
  genuinely external caller declare a `Policy` in YAML and compile it
  into the exact type `WithPolicy` accepts, without importing
  `internal/policy` — see [Configuring a Policy](#configuring-a-policy)
  above and [ADR 0008](docs/adr/0008-policy-config-boundary.md).
  Promoting `anomaly.Config`/`trust.Config`/`Store` the same way is a
  reasonable next step once a concrete external consumer needs it.
- **Declarative *alert* configuration doesn't exist.** `alert.Rule`s
  (see the [`alert`](docs/DOMAIN.md#alert) domain model) are
  constructed in Go only; there is no YAML equivalent of
  `config.PolicyConfig` for alert rules yet, and no CLI or OTel
  Collector processor integration for the policy config that does
  exist — see
  [`docs/ROADMAP.md` § v0.5](docs/ROADMAP.md#v05--policy--configuration).
- **Single-tenant.** Baseline/fingerprint keys are already scoped by
  `(ActorID, Environment)`, but there is no multi-tenant access control
  — that's explicitly a Trustvian Control/Cloud concern, not core-engine
  scope.

## Development

```bash
go build ./...
go vet ./...
go test -race ./...
gofmt -l .   # must produce no output
```

See [`CLAUDE.md`](CLAUDE.md) for the full development guide, and
[`.claude/rules/`](.claude/rules/) for Go, architecture, testing, and
security conventions specific to this codebase.

## License

[Apache License 2.0](LICENSE)
