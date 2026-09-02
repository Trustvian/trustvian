# CLI Guide

```bash
go install github.com/Trustvian/trustvian/cmd/trustvian@latest
# or: make build   (from a clone; produces bin/trustvian)
```

```
trustvian analyze <events.json>        Score each event and print a report
trustvian baseline build <events.json> Learn a baseline from a corpus of events
trustvian help
```

The CLI uses a built-in starter policy (block on high/critical risk,
alert on medium, allow otherwise) — see
[`cmd/trustvian/policy.go`](../cmd/trustvian/policy.go) and
[Go SDK Guide § the public/internal boundary today](sdk-guide.md#the-publicinternal-boundary-today)
for why this isn't yet configurable via a flag.

## `trustvian analyze`

Scores every event in the file and prints one report per event.
`analyze` is strictly read-only across the whole file — even multiple
events in one file never build on each other's baseline (each is
scored as if it were the actor's first-ever observation). If you want
to see a baseline mature over many events, use the Go SDK — see
[Go SDK Guide § watching trust mature](sdk-guide.md#watching-trust-mature).

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

- `Service` — `Actor.ID`
- `Anomaly` — `Result.Anomaly.Score`, `[0,1]`
- `Trust` — `Result.Trust.Score`, `[0,1]`
- `Risk` — `Result.Trust.Risk`, one of `LOW`/`MEDIUM`/`HIGH`/`CRITICAL`
- `Detected:` — one line per `Result.Anomaly.Contributors[i].Detail`, only shown when at least one signal fired
- `Decision` / `Reason` — `Result.Decision` and `Result.Explanation.Reason`

More worked examples (agent tool calls, service-to-service, a sudden
sensitive-destination access): [Use Cases](use-cases.md).

## `trustvian baseline build`

Replays a corpus of events through the *same gated* `Analyze`+`Observe`
path as live traffic — it does not blindly trust every event in the
file, on purpose (see
[Go SDK Guide § Observe and learning](sdk-guide.md#observe-and-learning)).
Prints a summary instead of a per-event report.

```bash
trustvian baseline build corpus.json
```

```
Trustvian Baseline Build

Events processed: 4
Learned:          3
Skipped:          1 (flagged by policy; not learned from)
```

**This result does not persist.** The only `Store` implementation
today is in-memory, so a `Baseline` doesn't survive the process exiting
— running `baseline build` and then a separate `analyze` invocation
gets you a *fresh*, empty baseline in the second process. `baseline
build` is useful for proving out the learning mechanism on a fixture,
not (yet) for pre-seeding a baseline a later CLI invocation can use. A
real deployment builds its baseline once, inside the long-running
process that then serves `Analyze` calls — i.e., via the Go SDK, not
this CLI. See [Limitations](../README.md#limitations).

## Event JSON format

`<events.json>` is a JSON array of events (even for one event, wrap it
in `[...]`):

```json
[
  {
    "id": "evt-1",
    "timestamp": "2026-01-01T12:00:00Z",
    "actor": {
      "id": "svc-payment",
      "type": "service",
      "identity_confidence": 0.95
    },
    "operation": {
      "category": "http",
      "name": "POST /payment",
      "direction": "inbound"
    },
    "target": {
      "name": "payment-db"
    },
    "context": {
      "environment": "production"
    },
    "attributes": {
      "duration_ms": 42,
      "error": false
    }
  }
]
```

| Field | Required | Notes |
|---|---|---|
| `id` | yes | any unique string |
| `timestamp` | yes | RFC 3339 |
| `actor.id` | yes | |
| `actor.type` | yes | `service`, `user`, `service_account`, `ai_agent`, `device`, `unknown` |
| `actor.identity_confidence` | yes | `[0, 1]` |
| `operation.category` | yes | `http`, `db`, `rpc`, `tool`, `external` |
| `operation.name` | yes | e.g. `"POST /payment"`, `"search_customer"` |
| `operation.direction` | no | `inbound`, `outbound`, or omit |
| `target.name` | no | destination service/DB/host |
| `context.environment` | no | |
| `context.trace_id` / `context.span_id` | no | for OTel correlation |
| `attributes.duration_ms` | no | feeds the latency anomaly signal |
| `attributes.error` | no | feeds the error anomaly signal |
| `attributes.*` (other keys) | no | passed through, available to a custom `ContextRisk` function |

Full Go-side type reference: [Go SDK Guide § the Event type](sdk-guide.md#the-event-type).

## Makefile shortcuts

```bash
make demo             # trustvian analyze against a bundled fixture
make baseline-demo    # trustvian baseline build against a bundled corpus
make run ARGS="analyze path/to/events.json"
```
