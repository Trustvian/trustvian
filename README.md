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

This is an early, single-tenant MVP. The core pipeline, Go SDK, CLI, and
an OpenTelemetry adapter are implemented and tested. Not yet built:
persistent baseline storage (the current store is in-memory only — see
[Limitations](#limitations)), the OpenTelemetry Collector processor,
sequence/ML-based anomaly detection, and everything under Trustvian
Control/Cloud (dashboard, multi-tenancy, SSO). See
[`trustvian-project-spec.md`](trustvian-project-spec.md) for the full
long-term vision and [`CLAUDE.md`](CLAUDE.md) for the engineering
conventions this repository follows. For guides, worked examples, and
four real-world use cases with verified input/output, see
[`docs/`](docs/README.md).

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
`trustvian.WithContextRisk`) — see [Limitations](#limitations) for the
current constraint on using them from outside this module.

## OpenTelemetry

The core engine has no OpenTelemetry dependency at all. `internal/otel`
is a self-contained adapter that maps a finished span
(`sdktrace.ReadOnlySpan`) into an `event.Event` using standard semantic
conventions (HTTP, DB, RPC, `deployment.environment.name`,
`service.name`) plus four documented `trustvian.*` override attributes
for what no convention covers yet. An OpenTelemetry Collector processor
is planned as a separate deliverable — it needs the heavier
collector-builder toolchain, which is deliberately kept out of this
module's dependency graph.

## Project layout

```
trustvian/
├── trustvian.go, engine.go, options.go, result.go   # public SDK (root package)
├── event/               # public domain vocabulary: Event, Actor, Operation, Target, Context
├── cmd/trustvian/        # CLI
├── internal/
│   ├── features/          # Event -> stable/volatile Features
│   ├── fingerprint/       # Features -> deterministic Fingerprint
│   ├── baseline/          # statistical model (EWMA mean/variance, maturity)
│   ├── store/              # Baseline persistence port + in-memory implementation
│   ├── anomaly/            # Features + Baseline -> Anomaly (noisy-OR combination)
│   ├── trust/               # Anomaly + identity + context -> Trust + RiskLevel
│   ├── policy/               # data-driven rule evaluation -> Decision
│   └── otel/                  # OpenTelemetry span -> Event adapter
└── trustvian-project-spec.md, CLAUDE.md   # vision and engineering conventions
```

Only the root package and `event` are importable from outside this
module — everything else is intentionally `internal/`. See
[`.claude/rules/architecture.md`](.claude/rules/architecture.md) for
the reasoning.

## Limitations

- **No persistent baseline storage yet.** The only `Store`
  implementation is in-memory; a `Baseline` does not survive a process
  restart. `trustvian baseline build` therefore only proves out its
  mechanism within a single CLI invocation — a real deployment builds
  its baseline once, inside the long-running process that then serves
  `Analyze` calls.
- **Policy and thresholds aren't yet a public package.** `WithPolicy`,
  `WithAnomalyConfig`, `WithTrustConfig`, `WithStore`, and
  `WithContextRisk` take types from this module's `internal/` packages,
  so a separate Go module can construct an `Engine` and call
  `Analyze`/`Observe` today, but can't yet supply custom configuration
  from outside this repository. Promoting those types to a public
  package is a reasonable next step once an external consumer actually
  needs it.
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
