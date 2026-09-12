# Getting Started

## Install

CLI:

```bash
go install github.com/Trustvian/trustvian/cmd/trustvian@latest
```

Or clone and use the Makefile:

```bash
git clone https://github.com/Trustvian/trustvian.git
cd trustvian
make build      # -> bin/trustvian
make demo       # analyze the bundled example fixture
make baseline-demo
```

Go SDK, in your own module:

```bash
go get github.com/Trustvian/trustvian
```

## Your first analysis (CLI)

Create `event.json`:

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

```bash
trustvian analyze event.json
```

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

`Anomaly: 1.00` looks alarming on its own — this is the *first time*
Trustvian has ever seen this actor, so it's maximally novel by
definition. But `Trust: 0.95` and `Decision: ALLOW` show the full
picture: novelty on its own, from an identity Trustvian has high
confidence in, isn't treated as dangerous. This split is deliberate —
see [Architecture](ARCHITECTURE.md#cold-start-two-numbers-not-one) and
[Use Cases](use-cases.md) for why.

Full command reference: [CLI Guide](cli-guide.md).

## Your first analysis (Go SDK)

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
	// 0.95 low allow

	engine.Observe(context.Background(), result) // safe to call unconditionally
}
```

Full reference and a worked multi-event example: [Go SDK Guide](sdk-guide.md).

## Configuring a custom policy from a file

The default `Engine` above always resolves to `observe_only` — real
`ALLOW`/`BLOCK`/`REQUIRE_APPROVAL` differentiation needs a configured
`Policy`. As of `v0.5`, you can load one from a YAML file without
writing Go:

```go
cfg, err := config.LoadFile("trustvian.yaml")
if err != nil {
	panic(err)
}
p, err := config.CompilePolicy(cfg)
if err != nil {
	panic(err)
}
engine := trustvian.NewEngine(trustvian.WithPolicy(p))
```

See [Policy Guide § Loading a Policy from a YAML
file](policy-guide.md#loading-a-policy-from-a-yaml-file) for the file
format and what strict decoding rejects. The CLI can load the same
file directly, without any Go code:

```bash
trustvian analyze --config trustvian.yaml event.json
```

See [CLI Guide § --config](cli-guide.md#--config-path).

## Enabling behavioral signals (`v0.6`/`v0.7`)

Sequence/Markov/delegation detection (`transition_deviation`,
`ngram_deviation`, `markov_surprisal`, `delegation_deviation`, ...)
all ship **opt-in, disabled by default** — every existing user's
behavior stays unchanged until deliberately configured. As of
[task 033](tasks/033-v07-stabilization-release-gate.md), enabling them
follows the identical pattern as Policy above, through a second,
independent document:

```go
cfg, err := config.LoadAnomalyFile("anomaly.yaml")
if err != nil {
	panic(err)
}
ac, err := config.CompileAnomaly(cfg)
if err != nil {
	panic(err)
}
engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(ac))
```

```bash
trustvian analyze --anomaly-config anomaly.yaml event.json
```

See [Anomaly Configuration Guide](anomaly-config-guide.md) for the
full field reference (every weight/threshold, its default, and its
valid range).

## Where to next

- Writing custom rules (`BLOCK`/`ALERT`/`ALLOW` decisions): [Policy Guide](policy-guide.md)
- Enabling anomaly signals: [Anomaly Configuration Guide](anomaly-config-guide.md)
- Feeding in OpenTelemetry spans: [OpenTelemetry Adapter](OPENTELEMETRY.md)
- Four worked real-world scenarios: [Use Cases](use-cases.md)
- How the pieces fit together: [Architecture](ARCHITECTURE.md)
