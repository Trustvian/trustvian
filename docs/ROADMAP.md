# Roadmap

Status against the phases in
[`trustvian-project-spec.md` § Roadmap](../trustvian-project-spec.md#18-roadmap),
reconciled with what's actually in this repository today (verified by
listing packages, tests, and files — not by memory). "Implemented"
means shipped and tested; "In progress" means partially built;
"Planned" means designed for but not started; "Future/experimental" is
explicitly out of scope for now per project instructions (no ML, no
Trustvian Control/Cloud, no Redis/PostgreSQL/Kafka/Kubernetes).

## Implemented

**Phase 0 — Foundation**
- Go module, repository, Apache 2.0 license
- Core domain models (`event`, `internal/features`,
  `internal/fingerprint`, `internal/baseline`, `internal/anomaly`,
  `internal/trust`, `internal/policy`)
- Unit tests (86–100% statement coverage per package) and a race-tested
  concurrency suite for `internal/store`/`internal/baseline`
- Benchmark framework covering every pipeline stage (see
  [PERFORMANCE.md](PERFORMANCE.md))
- Documentation set (this `docs/` tree, `README.md`, `CLAUDE.md`,
  ADRs)

**Phase 1 — Behavioral Engine**
- Event model (`event.Event`)
- Feature extraction (`internal/features`, stable/volatile split)
- Behavioral fingerprint (`internal/fingerprint`, deterministic
  per-shape identity)
- Baseline (`internal/baseline`, EWMA mean/variance/error-rate,
  maturity counting)
- Anomaly score (`internal/anomaly`, noisy-OR over four signals)
- Trust score (`internal/trust`, multiplicative, confidence-weighted)
- Explainable decisions (`Anomaly.Contributors`,
  `policy.Explanation`, all the way to `Result`)

**Phase 3 — Policy Engine (partial)**
- Policy model (`policy.Policy`/`Rule`/`Condition`) and rule evaluation
- All six decisions: `ALLOW`, `OBSERVE_ONLY`, `ALERT`, `CHALLENGE`,
  `REQUIRE_APPROVAL`, `BLOCK` (the spec names `ALLOW`/`BLOCK`/`CHALLENGE`
  as the minimum; all six from
  [`trustvian-project-spec.md` § 17](../trustvian-project-spec.md#17-policy-engine)
  are implemented)
- Fail-closed evaluation (see [SECURITY.md](SECURITY.md#policy-bypass))

**CLI and SDK (not explicitly phased in the spec, but shipped)**
- `trustvian analyze`, `trustvian baseline build` (see
  [CLI Guide](cli-guide.md))
- Go SDK: `Engine`, `NewEngine`, `Analyze`, `Observe`, functional
  options (see [Go SDK Guide](sdk-guide.md))

**Phase 2 — OpenTelemetry (partial)**
- Span → `Event` adapter (`internal/otel.EventFromSpan`), standard
  semantic conventions plus four documented override attributes (see
  [OPENTELEMETRY.md](OPENTELEMETRY.md))

## In progress / partial

- **Phase 2 — OpenTelemetry attributes.** The four *inbound* override
  attributes (`trustvian.actor.id`, `.actor.type`,
  `.identity.confidence`, `.operation.category`) are implemented. The
  spec's six *outbound* enrichment attributes
  (`trustvian.anomaly.score`, `.trust.score`, `.risk.level`,
  `.decision`, `.fingerprint.id`, `.behavior.id`) are **not yet
  implemented** — nothing in this repository writes them back onto
  telemetry. See [OPENTELEMETRY.md](OPENTELEMETRY.md#trustvian-output-attributes-not-yet-implemented).
- **Phase 4 — AI-agent security.** `ActorTypeAIAgent` and
  `OperationCategoryTool` exist and are exercised end-to-end (see
  [Use Cases § AI-agent security](use-cases.md#ai-agent-security)) —
  an agent's tool calls flow through the exact same
  fingerprint/baseline/anomaly/trust/policy pipeline as any other
  actor. What's specific to agents from the spec's Phase 4 list —
  tool-*sequence* analysis, dedicated agent-session tracking, human
  approval *workflows* (as opposed to the `REQUIRE_APPROVAL` decision
  value, which exists) — is not implemented. Per project instructions,
  a dedicated AI-agent security layer is explicitly not being built
  ahead of need; today's uniform-pipeline treatment is the intended
  MVP shape, not a placeholder.
- **Phase 5 — CLI.** `analyze` and `baseline build` exist; the spec's
  additional subcommands (`fingerprint service`, `policy test`,
  `agent start`, `version`) are not implemented.
- **Phase 5 — Documentation.** Substantially complete as of this pass;
  ongoing as the implementation grows.

## Planned (designed for, not started)

- **Persistent `Store` implementation.** The `Store` port
  (`internal/store.Store`) is designed specifically so this is
  additive — see [ADR 0004](adr/0004-narrow-store-port-in-memory-only.md).
  No specific backend is chosen; per project instructions, Redis/Postgres
  are explicitly not being implemented ahead of a concrete need.
- **Policy versioning and a `policy test` CLI command** (Phase 3).
- **OTel Collector processor** (Phase 2) — a separate, heavier module
  (needs `otelcol-builder`); see
  [OPENTELEMETRY.md](OPENTELEMETRY.md#the-otel-collector-processor-planned-separate-module)
  and [ADR 0003](adr/0003-opentelemetry-adapter-single-module.md).
- **Sequence-based anomaly detection** (n-gram/Markov models over
  operation order) — mentioned in the original spec's signal-processing
  section as a future algorithm; the current `internal/anomaly` is
  deliberately a single deterministic algorithm (see
  [ADR 0001](adr/0001-hexagonal-core-and-pipeline-shape.md)) until a
  second one has a concrete design.
- **Community contribution model** (`CONTRIBUTING.md`), CI/CD,
  container image, Kubernetes/Helm packaging (Phase 5).
- **Multi-tenant `TenantID` on `baseline.Key`** — the data model is
  already shaped for this addition; see
  [SECURITY.md § future multi-tenant isolation](SECURITY.md#future-multi-tenant-isolation).

## Future / explicitly out of scope for now

Per project instructions, these are not being implemented in the
current phase, regardless of how prominently they appear in the
long-term vision document:

- Trustvian Control (Phase 6): web dashboard, central API, service
  inventory, historical analytics.
- Enterprise features (Phase 7): multi-tenancy enforcement, RBAC, SSO,
  audit, SIEM/Kafka integration, HA/horizontal scaling.
- Any ML-based anomaly detection.
- Redis/PostgreSQL/Kafka/Kubernetes dependencies of any kind in the
  core engine — see [ARCHITECTURE.md](ARCHITECTURE.md) and
  CLAUDE.md.
