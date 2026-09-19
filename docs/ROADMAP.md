# Roadmap

Where Trustvian is going, and what has to be true before the open-source core
is called `v1.0`.

This document is deliberately forward-looking. What already shipped is in
[CHANGELOG.md](../CHANGELOG.md); how the system works is in
[ARCHITECTURE.md](ARCHITECTURE.md) and [DOMAIN.md](DOMAIN.md); why past
decisions were made is in [`adr/`](adr/); the specifications behind completed
work are in [`archive/tasks/`](archive/tasks/README.md).

Status vocabulary: **SHIPPED**, **CURRENT**, **NEXT**, **FUTURE**. Nothing
marked FUTURE is scoped, designed, committed, or dated.

## Product Direction

Trustvian is a **Behavioral Security & Trust Engine**: it turns runtime
behavior into an explainable trust score and a security decision.

```text
Event → Features → Fingerprint → Baseline → Anomaly → Trust → Policy → Decision
```

The question it answers is not one the surrounding stack already answers:

| Layer | Question |
|---|---|
| Identity | Who is this actor? |
| Observability | What did this actor do? |
| **Trustvian** | **Should this behavior be trusted?** |

Three consequences shape everything below. Identity is not behavior — a valid
credential keeps working while the actor behind it starts behaving unusually.
Observability is not trust — a trace records what happened, not whether it
should have. And static authorization is not behavioral confidence — a
permission granted once does not notice drift.

Direction, not feature list: deepen behavioral evidence, keep every decision
explainable and deterministic, and remain something an operator can run
themselves.

## Current State

**Current stable line: `v0.9.x`.** SHIPPED.

The pre-`v1.0` core provides:

- the behavioral pipeline above, with explainable contributors on every score
- learned per-actor baselines with copy-on-write semantics and gated learning
- anomaly signals including novelty, frequency, time-pattern, sequence
  (transition deviation and rarity, bounded 3-gram, first-order surprisal),
  and delegation deviation
- deterministic, fail-closed policy evaluation with recorded approval evidence
- alert evaluation and signed generic-webhook delivery
- a Go SDK, a CLI (`analyze`, `baseline`, `version`), and public `event`,
  `alert`, and `config` packages
- an inbound OpenTelemetry adapter and a standalone Collector processor
- in-memory, file, and PostgreSQL stores, with a versioned schema and
  transactional migration
- a reference Docker Compose deployment, health and readiness endpoints, and
  operational metrics
- documented backup, restore, and upgrade procedures with an automated
  recovery drill
- signed container images with SBOM and provenance attestations, published by
  an automated release pipeline

Not implemented, stated so the list above is not read too generously: no
centralized management plane, no multi-tenancy, no MCP server surface, no
machine-learning detection path, and no prompt- or content-level analysis.

## Released Milestones

One line each. Details are in [CHANGELOG.md](../CHANGELOG.md); the task
specifications behind each are in [`archive/tasks/`](archive/tasks/README.md).

| Release | What it established |
|---|---|
| `v0.1.0` | Behavioral core hardened, benchmarked, and published — pipeline, SDK, CLI, first persistent store |
| `v0.2.0` | OpenTelemetry maturation: outbound `trustvian.*` attributes and a real Collector processor |
| `v0.3.0` | Baseline and anomaly depth, including time-pattern deviation |
| `v0.4.0` | Alert and notification foundation: alert evaluation, severity, signed webhook sink |
| `v0.5.0` | Policy and configuration: declarative policy and alert configuration through a public `config` package |
| `v0.6.0` | Behavioral detection depth: order-aware sequence analysis, bounded 3-gram context, first-order surprisal |
| `v0.7.0` | AI-agent behavioral security: session, delegation, and approval context, and approval-aware policy |
| `v0.8.0` | Production runtime and storage: PostgreSQL persistence, reference deployment, durability hardening |
| `v0.9.0` | Operational readiness: CI quality gates, release artifacts, supply-chain signing, health endpoints, self-observability, backup and restore |

## v1.0 — Production-Ready OSS

**NEXT.** `v1.0.0` is a production-readiness milestone, not a feature release.
It adds no new detection capability by default. It is the point at which the
open-source core is stable enough that an operator can depend on it running
and a consumer can depend on its API.

The question it answers: *what must be true before we are comfortable calling
this stable and production-ready?* Everything below gates work that already
exists rather than adding scope.

### Release principle

`v1.0` hardens `v0.1`–`v0.9`. Anything discovered missing at gate time becomes
a task under the milestone it actually belongs to, not a `v1.0` exception. The
gate is verified by reading source, tests, and benchmarks — never assumed.

### Reliability and correctness

- Every package exercised by unit, integration, and end-to-end tests, with
  `go test -race ./...` clean across all three modules.
- Every documented formula or algorithm reproduced exactly by at least one
  test, extended to cover every signal added through `v0.6` and `v0.7`.
- Cross-stage behavior — `Analyze` and `Observe` together — covered by tests
  that run the real gated learning loop rather than hand-seeded baselines.
- The PostgreSQL store's concurrency and durability guarantees verified under
  contention, not only in the `-short` tier.

### Security hardening

The threat model and its test index are in [SECURITY.md](SECURITY.md);
vulnerability reporting is [.github/SECURITY.md](../.github/SECURITY.md). The
gate is that every threat there still has a passing test, extended to cover:

- baseline poisoning against the sequence and delegation signals
- fail-closed behavior at the configuration boundary for malformed input
- bounded per-fingerprint state for every signal that learns, so behavioral
  state cannot become a resource-exhaustion path
- credential handling for store connections and webhook secrets
- dependency and container vulnerability scanning gating the release

### API and configuration stability

`v1.0` creates compatibility expectations that `0.x` did not. Before the tag:

- a deliberate review of every exported symbol in the root package, `event`,
  `alert`, and `config`, since after `v1.0` removing one requires a major bump
- the configuration schema reviewed the same way, with a documented rule for
  adding fields compatibly
- CLI flags and output treated as an interface, with a stated compatibility
  scope
- the Collector processor's configuration surface reviewed alongside it
- the storage schema's migration and rollback path documented for every
  supported version transition, extending the
  [compatibility matrix](operations.md#compatibility-matrix)
- a written breaking-change policy stating what a major, minor, and patch
  release may change after `v1.0` — **published** as
  [Compatibility Contract](compatibility.md); the remaining gate work is
  confirming each surface it classifies still matches the code at
  release time

### Performance and resource safety

Measured numbers belong in [PERFORMANCE.md](PERFORMANCE.md). The gate is:

- every hot path benchmarked, including the sequence signals and the
  configuration load path
- allocation behavior on the common path understood and documented, so a
  regression is caught rather than discovered later
- memory bounded and documented for every structure that learns
- sustained-load behavior measured against the reference deployment, so
  contention and growth are known rather than assumed

### Operational readiness

Deployment, health, readiness, persistence, backup, restore, upgrade, and the
recovery drill are shipped and documented in [operations.md](operations.md).
The gate is that each is verified against the release candidate rather than
trusted from a prior release, and that an upgrade from the previous stable
line is proven to preserve learned state.

### Observability and diagnostics

Operational metrics exist and are documented in
[observability.md](observability.md). The gate is that an operator can answer,
from the running system alone, whether Trustvian is healthy, whether it is
learning, and why a specific decision was made — without attaching a debugger
or reading the source.

### Documentation and adoption

Install, configure, integrate, deploy, operate, troubleshoot, and upgrade each
have a verified document, and every command and example in them runs against
the released version. The public API has a documented stability scope, and a
new adopter reaches a first analysis without reading the architecture first.

### Supply chain and release readiness

Release automation, signing, SBOM, and provenance are shipped and described in
[supply-chain.md](supply-chain.md) and the
[release guide](release-guide.md). The gate is that a downloaded artifact and
a published image can be verified end to end by a third party following only
the public documentation.

### Exit criteria

`v1.0.0` is tagged when all of the following hold, each demonstrable rather
than asserted:

1. The full gate set passes against the candidate commit, in CI, on the tagged
   source.
2. No known correctness, security, or data-durability defect is open against
   shipped behavior.
3. The public API and configuration review is complete and its outcome
   documented.
4. An upgrade from the previous stable line preserves learned state, proven by
   test.
5. Every document named above is accurate against the candidate, with its
   examples executed.
6. A published artifact and image verify from the public documentation alone.
7. The breaking-change policy is written and published.

### Non-goals

`v1.0` is **not** gated on Trustvian Control, on an MCP surface, on any
enterprise capability, or on anything under [Beyond v1.0](#beyond-v10). It is
not gated on Kafka, Redis, or Kubernetes without a milestone of their own. It
does not require new detection signals; adding one is a minor release, before
or after `v1.0`.

## Beyond v1.0

**FUTURE. Nothing in this section is scoped, designed, committed, or dated.**
No task file exists for any of it. These are credible directions consistent
with the product, not planned work, and no version numbers beyond `v1.0` are
assigned to them.

### Behavioral intelligence

Deeper behavioral evidence, still deterministic and explainable: richer
sequence context beyond the fixed 3-gram, cross-session reasoning, baselines
that age gracefully, and numeric baselines for volume and rate. Any of these
must stay reproducible and explainable, and none may make machine learning a
dependency of the core detection path.

### AI agent security

Foundations shipped in `v0.7`: agents are behavioral actors analyzed by the
same pipeline, with session, delegation, and approval context. Future
direction extends that rather than replacing it — multi-hop delegation, tool
and MCP call behavior as first-class observed events, and behavioral
resource-abuse detection.

What this does not imply: Trustvian does not authenticate delegation claims,
verify approval provenance, or inspect prompt or completion content. Those
stay outside the deterministic core by design.

### Runtime Identity & Provenance

Workload and agent provenance as behavioral evidence — where an actor runs and
what it was built from — treated the way identity confidence already is: an
input Trustvian consumes, never one it computes.

### Integrations and adapters

Additional inbound and outbound surfaces, each following the adapter shape
[ADR 0003](adr/0003-opentelemetry-adapter-single-module.md) established:
framework middleware, additional alert sinks, policy-engine interoperability.
The core stays unaware of every adapter.

### Trustvian MCP

Trustvian exposing *itself* as an MCP server, so an agent platform could query
behavioral trust directly. Specified in
[015 — Trustvian MCP](tasks/015-trustvian-mcp.md), which remains
unimplemented.

This is distinct from observing the MCP tool calls an agent makes, which is
ordinary detection work. Neither depends on the other, and **neither is
required for `v1.0`**.

### Trustvian Control

An organizational-scale management and governance layer, considered only after
the OSS core has demonstrated real-world value. Specified as a placeholder in
[016 — Trustvian Control](tasks/016-control.md), which remains unimplemented.

Control would consume the OSS core as an ordinary external dependency and
never fork or reimplement it. **It is not required for `v1.0`**, and nothing
an operator needs in production waits on it.

### Managed and cloud direction

A managed offering is a possible future direction, not committed scope. No
availability, timeline, or terms are stated here.

### Future research

Research only, with no concrete design or consumer: machine-learning and
graph-based anomaly detection, kept explicitly optional and never a dependency
of the core; and prompt- or content-level security, which structurally
requires a model the deterministic core is designed not to need. If either is
ever built, it is a separate optional adapter producing ordinary
`event.Event` values, not a change to the core.

## The OSS / Enterprise product boundary

This governs every item above. **Trustvian OSS is a complete, standalone,
production-usable behavioral security product.** Nothing an operator needs to
detect, score, decide, alert on, integrate, or run Trustvian in production is
withheld for a paid product.

```text
Detect      Event, Features, Fingerprint, Baseline, Anomaly
Score       Trust, Risk
Decide      Policy, Decision, Explainability
Alert       Alert evaluation, notification, webhook delivery
Integrate   Go SDK, CLI, OTel adapter, Collector processor
Run         Persistence, deployment, health, self-observability, hardening
```

A future commercial layer's job is organizational scale and governance **on
top of** a fully capable core — centralized management across many
deployments, multi-tenancy, access control, fleet-wide policy, audit and
compliance workflows, and managed operation.

The dividing question is never "is this valuable enough to withhold from OSS".
It is "does this only make sense at organizational scale, across many
deployments or tenants", or "is this an operations and compliance concern
orthogonal to detection itself". Detecting, scoring, deciding, and alerting on
one deployment's behavior is OSS, however sophisticated the detection becomes.

## Roadmap principles

- **Deterministic before statistical, statistical before ML.** Explainability
  is a product requirement, not a preference, and ML never becomes a
  dependency of the core detection path.
- **Evidence, not verdicts.** Trustvian quantifies and explains deviation;
  policy decides what to do about it.
- **Small vertical slices.** A milestone is a sequence of independently scoped
  tasks, each with its own tests and acceptance criteria.
- **Verified, not assumed.** Statements about what exists are checked against
  source, tests, and benchmarks.
- **No speculative abstraction.** An interface arrives with its second
  implementation, not before it.
- **Trustvian consumes identity, it does not compute it.** Behavior can reduce
  trust; it never retroactively re-decides who an actor is.

## Related

- [CHANGELOG.md](../CHANGELOG.md) — what shipped, per release
- [Architecture](ARCHITECTURE.md) — system shape and boundaries
- [Domain Model](DOMAIN.md) — the concepts this roadmap refers to
- [Security Model](SECURITY.md) — threats considered and tested
- [Performance](PERFORMANCE.md) — measured results
- [Task specifications](tasks/README.md) — active work; completed work is
  under [`archive/tasks/`](archive/tasks/README.md)
