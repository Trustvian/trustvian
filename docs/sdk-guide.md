# Go SDK Guide

```bash
go get github.com/Trustvian/trustvian
```

## The `Event` type

`event.Event` (package `github.com/Trustvian/trustvian/event`) is the
one type every caller constructs. It's a generic shape that covers
HTTP calls, service-to-service RPC, database operations, external
destinations, and AI-agent tool calls uniformly — see
[Use Cases](use-cases.md) for one worked example of each.

```go
type Event struct {
	ID         string
	Timestamp  time.Time
	Actor      Actor
	Operation  Operation
	Target     Target          // optional
	Attributes map[string]any  // optional
	Context    Context         // optional
}

type Actor struct {
	ID                 string
	Type               ActorType // service | user | service_account | ai_agent | device | unknown
	IdentityConfidence float64   // [0,1] — an input Trustvian trusts, not something it computes
}

type Operation struct {
	Category  OperationCategory  // http | db | rpc | tool | external
	Name      string             // e.g. "POST /payment", "SELECT accounts", "search_customer"
	Direction OperationDirection // inbound | outbound | "" (optional)
}

type Target struct {
	Name string // the destination: a service, database, or host
}

type Context struct {
	Environment string
	TraceID     string
	SpanID      string
}
```

`Event` carries `json` struct tags (snake_case field names) so it can
be read from a file — this is what the CLI does; see
[CLI Guide](cli-guide.md#event-json-format).

Two attribute keys have special meaning if present in `Attributes`,
picked up by `internal/features`:

| Key | Type | Meaning |
|---|---|---|
| `duration_ms` | `float64`/`int`/`int64` | operation latency, feeds the anomaly latency signal |
| `error` | `bool` | whether the operation errored, feeds the anomaly error signal |

Call `ev.Validate()` (or just call `Analyze`, which does this for you)
to check required fields are set — `ID`, `Timestamp`,
`Actor.ID`/`Type`/`IdentityConfidence`, `Operation.Category`/`Name`.
`Target`, `Attributes`, and `Context` are optional.

## Constructing an `Engine`

```go
engine := trustvian.NewEngine()
```

With no options, you get: an in-memory `Store` (baselines don't
survive a process restart), a `Policy` with no rules and an
`OBSERVE_ONLY` default (never blocks, never silently allows), and
default anomaly/trust thresholds. This is enough to call `Analyze` and
`Observe` and get real, differentiated `Anomaly`/`Trust` output — you
just won't get a differentiated `Decision` until you configure a
`Policy` (see [Policy Guide](policy-guide.md)).

## Analyze

```go
result, err := engine.Analyze(ctx, ev)
```

Runs `ev` through the full pipeline. Strictly read-only — it never
writes to the `Store`, no matter how many times or in what order you
call it. Returns an error if `ev.Validate()` fails.

## `Result`

```go
type Result struct {
	Event       event.Event
	Features    features.Features   // internal type, fields readable
	Fingerprint fingerprint.Fingerprint
	BaselineKey baseline.Key
	Anomaly     anomaly.Anomaly     // Score, Confidence, Contributors []Signal
	Trust       trust.Trust         // Score, Risk, plus every input retained
	Decision    policy.Decision     // "allow" | "observe_only" | "alert" | "challenge" | "require_approval" | "block"
	Explanation policy.Explanation  // RuleName, Reason, MatchedDefault
}
```

Every field is readable from outside the module even though several of
the field *types* live under `internal/` — see
[Architecture § package boundaries](ARCHITECTURE.md#package-boundaries)
for why that's fine. In practice:

```go
fmt.Println(result.Trust.Score, result.Trust.Risk, result.Decision)
for _, signal := range result.Anomaly.Contributors {
	fmt.Println(signal.Name, signal.Value, signal.Detail)
}
```

## Observe and learning

```go
learned, err := engine.Observe(ctx, result)
```

Feeds `result` back into the `Baseline` — but only if `result.Decision`
is one where the action actually proceeded (`ALLOW`, `OBSERVE_ONLY`,
`ALERT`); anything held or stopped (`CHALLENGE`, `REQUIRE_APPROVAL`,
`BLOCK`) is silently skipped (`learned` comes back `false`, `err` is
`nil`). This is what makes it safe to call `Observe` after *every*
`Analyze` unconditionally — you never need to check the decision
yourself first.

## Watching trust mature

The following program calls `Analyze`+`Observe` in a loop for the same
actor doing the same operation 25 times, then analyzes one clearly
suspicious, unrelated event. It's a genuine external-module program —
compiled and run against this repository with no code living inside
it — output included verbatim, not hand-written:

```go
package main

import (
	"context"
	"fmt"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

func paymentEvent(id string) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "order-service",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.97,
		},
		Operation: event.Operation{Category: event.OperationCategoryDB, Name: "SELECT orders"},
		Target:    event.Target{Name: "orders-db"},
		Context:   event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 12},
	}
}

func main() {
	engine := trustvian.NewEngine()
	ctx := context.Background()

	for i := 1; i <= 25; i++ {
		result, _ := engine.Analyze(ctx, paymentEvent(fmt.Sprintf("warm-up-%d", i)))
		learned, _ := engine.Observe(ctx, result)
		if i == 1 || i == 10 || i == 20 || i == 25 {
			fmt.Printf("event %2d: confidence=%.2f trust=%.2f decision=%s learned=%v\n",
				i, result.Anomaly.Confidence, result.Trust.Score, result.Decision, learned)
		}
	}

	suspicious := paymentEvent("attack")
	suspicious.Actor.IdentityConfidence = 0.2
	suspicious.Operation = event.Operation{Category: event.OperationCategoryExternal, Name: "POST /exfil"}
	suspicious.Target = event.Target{Name: "unknown-host"}

	result, _ := engine.Analyze(ctx, suspicious)
	fmt.Printf("suspicious event: confidence=%.2f trust=%.2f decision=%s\n",
		result.Anomaly.Confidence, result.Trust.Score, result.Decision)
}
```

Output:

```
event  1: confidence=0.00 trust=0.97 decision=observe_only learned=true
event 10: confidence=0.45 trust=0.73 decision=observe_only learned=true
event 20: confidence=0.95 trust=0.92 decision=observe_only learned=true
event 25: confidence=1.00 trust=0.97 decision=observe_only learned=true
suspicious event: confidence=0.00 trust=0.20 decision=observe_only
```

Notice: **confidence climbs from 0 to 1** as the fingerprint matures
(20 observations is `anomaly.DefaultConfig().MinObservations`), and
**trust dips mid-ramp (0.73 at event 10) before recovering to 0.97** —
that dip is the categorical-novelty signal still partially
contributing while the fingerprint is only half-mature, exactly the
behavior [`internal/anomaly`](../internal/anomaly/anomaly.go) is
designed to produce. The suspicious event's `confidence=0.00` shows
it's being scored as a completely unrelated, unfamiliar fingerprint —
none of the 25 warm-up observations transferred to it, because they
were for a different `(Actor, Operation, Target)` shape entirely.

Every `decision` above is `observe_only` because this program used
`NewEngine()` with no `Policy` configured — see the next section for
why, and [Policy Guide](policy-guide.md) for how to get `ALLOW`/`BLOCK`
differentiation like the CLI examples in [Use Cases](use-cases.md).

## Options

```go
trustvian.NewEngine(
	trustvian.WithStore(customStore),
	trustvian.WithPolicy(customPolicy),
	trustvian.WithAnomalyConfig(customAnomalyConfig),
	trustvian.WithTrustConfig(customTrustConfig),
	trustvian.WithContextRisk(func(f features.StableFeatures) float64 { ... }),
)
```

### The public/internal boundary, today

`WithPolicy`, `WithAnomalyConfig`, `WithTrustConfig`, and
`WithContextRisk` take types (`policy.Policy`, `anomaly.Config`,
`trust.Config`, `features.StableFeatures`) that currently live under
`internal/`. That means: **code inside this repository can use every
option freely; a separate Go module that only depends on
`github.com/Trustvian/trustvian` cannot construct those values yet.**
This isn't an oversight — see
[Architecture § package boundaries](ARCHITECTURE.md#package-boundaries)
for the reasoning. Promoting those types to a public package is a
reasonable next step once an external consumer actually needs it.

In practice, today, this is how the CLI itself configures a policy
(from [`cmd/trustvian/policy.go`](../cmd/trustvian/policy.go) — real
code in this repository, not a hypothetical):

```go
func defaultPolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "block-high-risk",
				When:   policy.Condition{MinRiskLevel: trust.RiskHigh},
				Action: policy.DecisionBlock,
				Reason: "trust score indicates high or critical risk",
			},
			{
				Name:   "alert-medium-risk",
				When:   policy.Condition{MinRiskLevel: trust.RiskMedium},
				Action: policy.DecisionAlert,
				Reason: "trust score indicates elevated risk",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "risk within tolerance",
	}
}

func newEngine() *trustvian.Engine {
	return trustvian.NewEngine(trustvian.WithPolicy(defaultPolicy()))
}
```

This is exactly the `Decision` differentiation you see in every
[Use Cases](use-cases.md) example — those all go through the CLI's
`newEngine()`, i.e. this exact code.

See [Policy Guide](policy-guide.md) for the full `Rule`/`Condition`
reference and more examples.
