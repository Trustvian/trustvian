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
see [Architecture](architecture.md#cold-start-two-numbers-not-one) and
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

## Where to next

- Writing custom rules (`BLOCK`/`ALERT`/`ALLOW` decisions): [Policy Guide](policy-guide.md)
- Feeding in OpenTelemetry spans: [OpenTelemetry Adapter](opentelemetry.md)
- Four worked real-world scenarios: [Use Cases](use-cases.md)
- How the pieces fit together: [Architecture](architecture.md)
