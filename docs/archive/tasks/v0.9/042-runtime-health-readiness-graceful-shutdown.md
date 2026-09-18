# 042 — Runtime Health, Readiness & Graceful Shutdown

**Milestone:** v0.9 — Operational Readiness · **Depends on:**
[041](041-container-supply-chain-security.md) (the deployable artifact whose
runtime this makes observable) · **Blocks:** 043 (self-observability, which
observes the lifecycle state this establishes) · **Fourth slice of `v0.9`.**

## Objective

Give the long-lived Trustvian runtime explicit liveness and readiness
semantics, and guarantee bounded graceful shutdown that stops accepting
work, drains in-flight work, closes persistent resources, and exits
cleanly.

## What the audit found

The long-lived runtime is the **Collector** (`trustvian-collector`),
confirmed from code: it is the only executable that stays up and accepts
input. The `trustvian` CLI is batch.

| Capability | Exists | Current behavior | Gap |
|---|---|---|---|
| Liveness | ✗ | — | Nothing reports whether the runtime is alive |
| Readiness | ✗ | — | **Nothing reports whether PostgreSQL is usable** |
| Store health | ✗ | — | No way to probe a Store cheaply |
| Signal handling | ✅ | `otelcol.Collector.Run` notifies on `SIGINT`/`SIGTERM` | None — the framework owns it |
| Graceful stop | ✅ | `Graph.ShutdownAll` stops in topological order | None |
| Store close | ✅ | `processor.Shutdown` closes the pool (task 037) | Idempotence unverified |
| HTTP shutdown | ✗ | no server exists | Needed if health is served over HTTP |
| In-flight drain | ✅ | framework: receivers stop before processors | None |
| Bounded timeout | ~ | Collector bounds its own shutdown | A readiness probe needs its own bound |

Two of those are the crux, and both are **already correct** — verified in
the Collector's source, not assumed:

- `otelcol.Collector.Run` registers `signal.Notify` for `os.Interrupt` and
  `SIGTERM` unless graceful shutdown is disabled, which this binary does not
  disable.
- `Graph.ShutdownAll` stops components "in topological order so that
  upstream components are stopped before downstream components… so that each
  component has a chance to drain to its consumer". Receivers therefore stop
  before the processor, and in-flight spans drain through it.

**So this task must not add signal handling or a drain mechanism.** Doing so
would create a second shutdown owner competing with the framework — the
opposite of what § Graceful shutdown ownership requires. What is missing is
readiness, liveness, and a surface to observe them on.

## Liveness and readiness

Deliberately different questions:

**Liveness — "is this runtime process functioning and able to keep
executing?"** It does **not** consult PostgreSQL. A supervisor that
restarts a process because its database is down produces a restart storm
that cannot fix anything: the database is not in the process. Liveness is
false only once the runtime is shutting down or has stopped.

**Readiness — "can this instance safely process its configured workload
right now?"** It *does* consult the configured store. When PostgreSQL is
explicitly configured and unusable, readiness is false — never a silent
fallback to in-memory, which would violate `v0.8`'s fail-closed persistence
contract.

| Scenario | Live | Ready |
|---|---|---|
| Starting, before initialization completes | true | **false** |
| Normal operation, store usable | true | true |
| Explicit `type: memory` or `file` | true | true |
| PostgreSQL configured, unavailable | **true** | **false** |
| PostgreSQL recovered | true | true (no restart needed) |
| Graceful drain in progress | **false** | false |

Liveness going false at drain is deliberate: once the runtime has begun
shutting down it is not going to recover, and saying so lets a supervisor
stop routing and stop waiting.

## Architecture

```text
health.Health            state model — transport-agnostic, no HTTP knowledge
  ├─ Live()  / Ready()   evaluated on demand
  ├─ MarkReady()         called once initialization completes
  └─ MarkDraining()      called first thing in Shutdown
        ▲
        │ consulted by
health.Handler           the only HTTP-aware piece
        ▲
        │ served by
trustvianProcessor       owns the listener's lifecycle via Start/Shutdown
        │ probes
        └─ store (optional Ping capability)
```

The state model owns no transport. HTTP consumes it; a future metric or CLI
could consume the same value without touching it.

### Reaching the Store without new public API

Readiness must ask the store whether it is usable. The store arrives at the
processor as an opaque `store.Store` from `config.CompileStorage`, and
`internal/store` is not importable from the processor's separate module.

The solution adds **no public API and no method to `store.Store`**: the
PostgreSQL implementation gains a `Ping(ctx) error` method, and the
processor declares the one-method interface *locally* and type-asserts —
the same shape the existing `io.Closer` lifecycle assertion already uses,
and the same optional-capability idiom as `store.Freezer`.

```go
// In the consumer, naming no internal type:
type pinger interface{ Ping(context.Context) error }
if p, ok := s.(pinger); ok { err = p.Ping(ctx) }
```

A store that does not implement it — `InMemory`, `FileStore` — is ready by
construction: there is no external dependency that could be unavailable.
That is a correct answer, not a missing check.

### Why a dedicated listener

There is no existing HTTP server in this repository's code to attach to.
The OTLP receiver has one, but it belongs to the receiver and serves the
data plane; operational endpoints do not belong on it, and the Collector's
component model gives a processor no way to add routes to it anyway.

The Collector's contrib `healthcheckextension` was considered and rejected:
it reports *Collector* pipeline health and has no knowledge of whether
Trustvian's configured database is usable, which is the mandatory
requirement here. It would answer a different question convincingly.

## Endpoints

```text
GET /livez   → 200 {"status":"ok"}         | 503 {"status":"stopping"}
GET /readyz  → 200 {"status":"ok"}         | 503 {"status":"not_ready"}
```

One convention, no aliases. `/livez` and `/readyz` over
`/health/live` because they are unambiguous and conventional for generic
runtimes rather than tied to any orchestrator.

Bodies are minimal and machine-readable. They carry a status string and
nothing else — no DSN, no hostname, no SQL error, no configuration, no
actor or behavioral data. A probe endpoint is unauthenticated by design, so
its information content is kept deliberately near zero; the reason a probe
failed goes to the runtime's logs, where an operator already has access.

Unauthenticated, with no new authentication subsystem: the endpoints reveal
nothing worth protecting, and adding auth to a liveness probe is how probes
stop working.

## Configuration

One optional block on the processor's existing config, following `v0.5`'s
configuration architecture:

```yaml
processors:
  trustvian:
    health:
      endpoint: 0.0.0.0:13133        # default; omit the block to disable
      readiness_timeout: 2s          # bound on the store probe
```

Omitting `health:` disables the listener entirely, so every existing
Collector config keeps working unchanged. Two knobs, both with defaults,
because each answers a question an operator genuinely cannot answer
otherwise: where to bind, and how long a probe may take.

Shutdown timeout is deliberately **not** a new knob — the Collector already
bounds its own shutdown, and adding a second budget would create two
answers to one question.

## Failure semantics

| Condition | Behavior |
|---|---|
| Store probe fails | Readiness 503. Liveness unaffected. Reason logged, not returned. |
| Store probe exceeds `readiness_timeout` | Readiness 503 within the bound. Never hangs. |
| PostgreSQL unavailable at startup | Unchanged from `v0.8`: `CompileStorage` fails, so the processor fails to construct and the Collector refuses to start. This task does **not** convert a fail-closed startup into a start-but-not-ready runtime. |
| Schema incompatible | Unchanged: fail-closed at startup. Readiness never masks a migration or compatibility error. |
| Health listener fails to bind | `Start` returns the error; the Collector fails to start. A health surface that silently did not exist would be worse than none. |
| Shutdown errors | Aggregated with `errors.Join` and returned; never discarded. |

## Shutdown sequence

```text
SIGTERM / SIGINT          (framework: otelcol.Collector.Run)
  → Graph.ShutdownAll in topological order
      → receivers stop     — no new spans admitted        (framework)
      → in-flight spans drain to the processor            (framework)
      → trustvianProcessor.Shutdown:                      (this task)
          1. MarkDraining   — /readyz and /livez now 503
          2. health server graceful Shutdown(ctx)
          3. close the Store exactly once
      → exporters stop                                     (framework)
  → Run returns; main owns the exit status
```

Readiness flips **first**, before anything is torn down, so an external
observer sees "not ready" rather than a connection refusal. The health
server is stopped before the Store because its readiness probe uses the
Store — stopping it in the other order would let a probe race a closing
pool.

No `os.Exit` anywhere in runtime code; `Shutdown` returns errors upward and
the entrypoint owns the exit status.

## Cross-platform constraint

Task 040 releases `linux/{amd64,arm64}`, `darwin/{amd64,arm64}`, and
`windows/amd64`. This task adds **no signal handling of its own**, so it
introduces no build constraints and no platform-specific files — the
framework's portable handling is the only signal code in the process. The
release matrix must still cross-compile, and that is verified rather than
assumed.

## Non-Goals

- **No metrics of any kind** — no counters, histograms, Prometheus, latency
  or decision or error metrics. Those are 043. This task establishes
  lifecycle state that 043 may later observe; that is the whole overlap.
- **No resource-safety redesign** — pool sizing, queue bounds, and
  concurrency limits are 043's review.
- **No Kubernetes, no Helm, no probe manifests.** The endpoints are generic
  runtime endpoints; that an orchestrator could later consume them is a
  consequence, not the design target.
- **No backup/restore** (044).
- **No signal handling, no drain framework, no job abstraction** — the
  Collector already provides all three correctly.
- **No public API change**, and nothing added to `store.Store`.
- **No `HEALTHCHECK` in the official image**, and no shell tooling added to
  it — see § Container below.

## Container and Compose

The `v0.9` runtime image is distroless: no shell, no `curl`, no `wget`. A
Docker `HEALTHCHECK` requires an executable inside the image, so honoring
one would mean either adding a shell — discarding a deliberate `041`
security property — or adding a Trustvian health subcommand that exists
only to satisfy Docker.

Neither is worth it. Compose's `healthcheck` is therefore **not** added, and
the reasoning is documented instead: external HTTP probes against `/readyz`
are the intended mechanism, and every supervisor this task designs for
(Compose with an external prober, systemd, load balancers, monitoring) can
issue one. Revisit only if a concrete deployment needs an in-image probe.

The image gains `EXPOSE 13133` as documentation of the new port. Nothing
else about it changes.

## Tests

- **State model**: every transition, including concurrent readers, under
  `-race`.
- **Liveness**: live while running; still live when the store probe fails;
  not live once draining.
- **Readiness**: in-memory ready; a failing probe not ready; a probe that
  hangs returns within its bound; draining not ready.
- **Handler**: status codes and exact bodies; verified to contain no DSN,
  hostname, or error text.
- **Shutdown**: ordering proven with channels rather than sleeps —
  readiness flips before the store closes; store closes exactly once;
  duplicate shutdown neither panics nor deadlocks.
- **PostgreSQL integration**, reusing tasks 035/036's DSN-gated
  infrastructure with no second framework: reachable → ready; connection
  killed → not ready; recovered → ready again with no restart.

## Documentation

`docs/ROADMAP.md`, `CHANGELOG.md`, `docs/ARCHITECTURE.md` (the lifecycle
and health abstraction), `deployments/docker-compose/README.md` and
`docs/supply-chain.md` (the port and the deliberate absence of a
`HEALTHCHECK`), `processor/README.md` (the config block). `README.md` gains
no milestone status.

## Acceptance Criteria

1. Liveness and readiness are defined, separate, and implemented.
2. A PostgreSQL outage never makes liveness fail.
3. PostgreSQL configured and unavailable ⇒ not ready; never a silent
   fallback to in-memory.
4. Explicit in-memory or file storage can become ready.
5. Readiness is bounded by a configurable timeout and is cheap — one
   driver-level ping, no migration, no table scan, no state load.
6. Health responses expose no secrets, database detail, actor data, or
   behavioral state.
7. Startup is not ready until initialization completes; `v0.8` fail-closed
   startup and schema-compatibility behavior are unchanged.
8. Shutdown transitions readiness first, then stops the health server, then
   closes the Store exactly once; duplicate shutdown is safe.
9. Shutdown is bounded, and no `os.Exit` exists in runtime code.
10. Lifecycle tests are deterministic and race-clean.
11. PostgreSQL loss ⇒ not ready, and recovery ⇒ ready with no restart,
    proven against a real database.
12. All prior gates pass: root, module consistency, processor `GOWORK=off`,
    examples, PostgreSQL, release dry-run across all five targets,
    container build, and the vulnerability gates unchanged.
13. No metrics, no Kubernetes, no Helm, no public API change.
14. Documentation synchronized; `README.md` remains evergreen.
