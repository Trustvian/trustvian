# 037 — Reference Docker Compose Deployment

**Milestone:** v0.8 — Production Runtime & Storage · **Depends on:**
[034](034-production-store-contract-and-public-boundary.md) (public storage
selection), [035](035-postgresql-store-implementation.md) (the PostgreSQL
backend), [036](036-store-durability-concurrency-and-migration-hardening.md)
(its verified guarantees), [022](022-collector-config-integration.md) (the
processor's canonical-config precedent) · **Blocks:** 038 (`v0.8`
stabilization & release gate) · **Fourth slice of `v0.8`.**

## Objective

Answer, with something a stranger can run:

> Can a new OSS user clone Trustvian and run a realistic persistent
> Trustvian deployment locally with one documented Docker Compose
> workflow?

Tasks 034–036 built production persistence and proved it correct. None of
them demonstrated it *deployed*. This slice does, end to end:

```text
telemetry → Trustvian runtime → behavioral analysis
          → PostgreSQL baseline → restart → same learned state
```

## Why

`v0.8`'s objective is that "OSS should be deployable as a real production
system, not only a library and a CLI against a local file." Three slices
in, every guarantee is real but every demonstration is a Go test. A user
evaluating Trustvian cannot see persistent behavioral analysis working
without writing code first.

There is a sharper reason too. Task 034 found that `FileStore` had been
durable, tested, and *unreachable* for seven milestones because nothing
wired it up. A capability that is never exercised through the path a user
actually takes is a capability nobody can be sure works. Compose is that
path for deployment.

## Runtime decision: the Collector processor

The roadmap permits Trustvian to run either as a library inside a
consuming service or through the OTel Collector processor. **This
deployment uses the Collector processor**, and introduces **no new
Trustvian server**.

Reasons, from repository evidence rather than preference:

- It already exists and already runs. `processor/` is a real Collector
  processor with a buildable Collector binary at
  `processor/cmd/trustvian-collector`, and `processor/config.yaml`
  already documents an OTLP → Trustvian → debug pipeline.
- It is the only existing shape that is a **long-lived network service**.
  The CLI is batch; the SDK is a library. A Compose deployment needs
  something that stays up and accepts input, and inventing an HTTP daemon
  to get one would add a public network surface the architecture has
  deliberately not committed to.
- It already calls both `Analyze` **and** `Observe`, so learning actually
  happens and there is state for PostgreSQL to hold. A runtime that only
  scored would make the persistence demonstration vacuous.

## The gap this slice must close

The processor **could not** select PostgreSQL before this task. Two
distinct problems, both verified by reading the code rather than assumed:

1. **No storage configuration exists.** `processor.Config` has exactly one
   field, `Policy`. There is no `storage:` block, and
   `newTrustvianProcessor` calls `trustvian.NewEngine` with at most
   `WithPolicy` — never `WithStore`. Every Collector deployment therefore
   ran on the default in-memory store and lost every baseline on restart:
   the same class of gap task 034 found in the CLI.
2. **`processor/go.mod` requires `github.com/Trustvian/trustvian v0.5.0`**,
   which predates `config.StorageConfig` entirely. The package cannot even
   name the type it needs.

Closing (1) is the minimum integration wiring this task's objective
requires, and it must reuse the canonical model:

```text
processor config  →  config.StorageConfig  →  config.CompileStorage
                                           →  trustvian.WithStore
```

**No processor-specific PostgreSQL implementation, no processor-specific
DSN parsing, no second storage configuration model.** Task 022 already
established exactly this pattern for `Policy` — `decodePolicy` decodes
Collector's generic map into the real `config.PolicyConfig` using its own
`yaml` tags, then hands it to `config.CompilePolicy`. Storage mirrors it
line for line.

Closing (2) uses a `replace` directive back to the repository root, which
is the pattern `examples/go.mod` already uses for the same reason.
Writing `require github.com/Trustvian/trustvian v0.8.0` before that tag
exists would be a lie in a build file. **Task 038 owns the release-time
transition** (see § Release-time dependency transition).

## Scope

```text
deployments/docker-compose/
  compose.yaml                  the one canonical entry point
  .env.example                  local-development defaults, no real secrets
  README.md                     the quickstart
  collector.yaml                Collector pipeline + Trustvian config
  Dockerfile.collector          builds the Collector from repository source
  Dockerfile.demo               builds the demo producer
  smoke-test.sh                 deterministic, exits non-zero on failure

processor/config.go             + Storage field, + decodeStorage
processor/processor.go          + WithStore wiring, + Shutdown closes the pool
processor/go.mod                + replace => ../
processor/cmd/demo-producer/    the smallest deterministic OTLP producer
Makefile                        compose-up / compose-down / integration-postgres
```

**Location rationale.** `deployments/docker-compose/` rather than
`examples/`: `examples/` is a Go module of runnable Go programs, and this
is neither. The name says *reference deployment* without implying official
production infrastructure, and it leaves room for `deployments/` to gain
siblings later without relocating this one.

## Non-Goals

- **No Kubernetes, no Helm**, no operators, no StatefulSets.
- **No official image publishing.** Nothing is pushed; no
  `ghcr.io/trustvian/...` reference appears anywhere, because no such image
  exists. Everything builds locally from source. Official image and
  release packaging are `v0.9`.
- **No infrastructure creep.** No Redis, Kafka, NATS, RabbitMQ,
  ClickHouse, OpenSearch, Elasticsearch, Grafana, Prometheus, Jaeger,
  Tempo, or Loki. The deployment proves Trustvian, not an observability
  stack.
- **No release automation, SBOM, image signing, or vulnerability
  scanning.**
- **No HA, clustering, or multi-replica coordination.**
- **No new Trustvian HTTP server or network daemon.**
- **No health/readiness endpoint.** Compose health checks here are
  dependency startup ordering only; application health is `v0.9`.
- **No graceful-shutdown framework.** Existing `Close` semantics are
  used; nothing more is built.
- **No alert delivery expansion.** No Slack, Teams, PagerDuty, retry
  queues, deduplication, or escalation.
- **No new behavioral semantics, detector, or policy feature.**
- **No container hardening programme.** `v0.9`.
- **Not task 038.** This slice does not gate the release.

## Integration-test environment reuse

The roadmap intends this slice to become the home for tasks 035/036's
PostgreSQL integration environment. It does so **without touching a single
test**: the Compose PostgreSQL service exposes a host port, and the
existing `TRUSTVIAN_TEST_POSTGRES_DSN` variable points at it. No new gating
mechanism, no test rewritten, no test weakened.

```bash
docker compose -f deployments/docker-compose/compose.yaml up -d postgres
export TRUSTVIAN_TEST_POSTGRES_DSN='postgres://trustvian:trustvian@localhost:5433/trustvian?sslmode=disable'
go test -race ./...
```

Host exposure is local-development behavior, documented as such, and is
the one deliberate exception to § Least exposure below.

## Release-time dependency transition (for task 038)

`processor/go.mod` carries `replace github.com/Trustvian/trustvian => ../`
so it builds against unreleased `v0.8` APIs. Task 038 must decide, and
record, one of:

- **Bump and drop the replace** — after `v0.8.0` is tagged, set
  `require github.com/Trustvian/trustvian v0.8.0` and remove the replace,
  so the processor module is independently consumable at a released
  version; or
- **Keep the replace** — accept that the processor is built from the
  repository rather than consumed as a versioned module.

This task deliberately does not choose: the decision depends on whether
`v0.8.0` is tagged, which is 038's business. What it does do is make the
choice explicit rather than leaving a stale `v0.5.0` pin nobody notices.

## Security posture

The reference deployment is **local and illustrative, not hardened
production orchestration**, and its documentation says so plainly.

- Credentials are obvious development placeholders in `.env.example`,
  never real secrets, and never presented as production-ready.
- `sslmode=disable` appears because both ends are inside one Compose
  network on one machine. Production uses TLS through the DSN.
- The DSN is never logged. Task 035's credential-redaction guarantees
  apply unchanged, because the processor reaches PostgreSQL through the
  same `config.CompileStorage` path everything else does.
- Only the ports the workflow needs are published.

## Acceptance Criteria

1. One canonical Compose file; no `.dev`/`.prod`/`.override` variants.
2. `docker compose config` succeeds.
3. PostgreSQL is pinned to an explicit major version consistent with task
   036's tested support, and uses a named volume.
4. No real secret is committed; local defaults come from `.env.example`.
5. The processor selects PostgreSQL through `config.StorageConfig` +
   `config.CompileStorage`, with no parallel storage model and no
   processor-side database construction.
6. Real OTLP telemetry reaches Trustvian and is analyzed against
   PostgreSQL-backed state.
7. Learned baseline state is inspectable with a documented SQL command
   that requires no JSONB decoding.
8. The schema version is inspectable with a documented command.
9. A restart preserves the learned baseline; `down` + `up` with the volume
   retained preserves it too; `down -v` destroys it, documented.
10. PostgreSQL configured but unavailable fails the runtime closed — never
    a silent fall back to in-memory.
11. A deterministic smoke test exercises the whole workflow and exits
    non-zero on failure.
12. The documented workflow succeeds from a clean state, with no
    dependence on local build artifacts or developer-specific paths.
13. Task 035/036 integration tests run against the Compose PostgreSQL with
    no test modified.
14. `InMemory` remains the default when no storage is configured;
    `FileStore` remains supported; no behavioral semantics change.
15. No raw-event warehouse; no prohibited infrastructure; no Kubernetes or
    Helm; no image publishing.
16. Root, processor, and examples quality gates all pass; no new Core
    production dependency.
