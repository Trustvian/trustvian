# Trustvian — Open Source Behavioral Security & Trust Engine

**Category:** Behavioral Security & Trust Engine  
**Primary tagline:** **Trust the Behavior.**  
**Secondary tagline:** **From Behavior to Trust.**  
**AI-agent positioning:** **Don't just authenticate your agents. Trust their behavior.**

## Current Implementation Status

This document is Trustvian's long-term product vision. It is not a
description of what's built today — for that, see
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) (system architecture,
package structure, dependency direction), [`docs/DOMAIN.md`](docs/DOMAIN.md)
(the actual domain model), [`docs/SECURITY.md`](docs/SECURITY.md),
[`docs/PERFORMANCE.md`](docs/PERFORMANCE.md), and
[`docs/ROADMAP.md`](docs/ROADMAP.md) (what's implemented vs. planned vs.
future). Where this document's architecture sketches (repository
layout, package names) differ from those docs, the `docs/` set is
authoritative — it describes the real, tested implementation; sections
below are retained as directional/aspirational context for where the
product is headed. Notably: the core engine follows a **hexagonal
architecture with no `pkg/` layer** (public API = root package +
`event`; everything else under `internal/`) rather than the `pkg/` +
`internal/` split sketched in §9 below — see
[ADR 0001](docs/adr/0001-hexagonal-core-and-pipeline-shape.md) and
[ADR 0002](docs/adr/0002-public-api-boundary.md) for why.

## 1. Vision

Trustvian is an open-source, Go-based behavioral security engine that transforms runtime telemetry and application behavior into actionable security decisions.

> **OpenTelemetry observes behavior. Trustvian evaluates whether that behavior should be trusted.**

Trustvian targets modern applications, APIs, distributed systems, microservices, AI agents, tool-calling agents, service-to-service communication, and runtime security.

## 2. Core Architecture

```text
Application / AI Agent
        |
        v
OpenTelemetry / OTLP
        |
        v
+---------------------------+
|      Trustvian Engine     |
|                           |
| Features                  |
| Fingerprinting            |
| Baseline                  |
| Anomaly Detection         |
| Risk / Trust Scoring      |
| Policy Engine              |
+---------------------------+
        |
        v
  ALLOW / CHALLENGE / BLOCK
```

## 3. OpenTelemetry Strategy

OpenTelemetry is the primary telemetry integration and distribution layer.

```text
Application
    |
    | OTLP
    v
OpenTelemetry Collector
    |
    v
Trustvian OTel Processor
    |
    v
Trustvian Engine
    |
    +-- Behavioral Analysis
    +-- Fingerprinting
    +-- Baseline
    +-- Anomaly Detection
    +-- Trust Scoring
    +-- Policy
    |
    +--> OTLP / SIEM / Trustvian Control
```

Suggested Trustvian attributes:

```text
trustvian.anomaly.score
trustvian.trust.score
trustvian.risk.level
trustvian.decision
trustvian.behavior.id
trustvian.fingerprint.id
```

## 4. Main Use Cases

### API behavioral anomaly

Normal:

```text
POST /payment
  -> Authenticate
  -> PaymentService
  -> FraudService
  -> Database
```

Abnormal:

```text
POST /payment
  -> Authenticate
  -> PaymentService
  -> AdminService
  -> Secrets
  -> External API
```

Example result:

```text
Trust Score: 0.27
Anomaly Score: 0.93
Risk: HIGH
Decision: BLOCK

Reasons:
- Unexpected service dependency
- Unexpected secret access
- Abnormal external communication
```

### AI-agent security

Normal:

```text
Agent
  -> search_customer
  -> get_order
  -> respond
```

Suspicious:

```text
Agent
  -> filesystem
  -> get_credentials
  -> external_http
  -> send_email
```

Trustvian evaluates runtime behavior instead of relying only on identity.

### Service-to-service security

A service normally communicates with a known set of services and databases. A sudden connection to a secrets manager or unknown external destination should increase anomaly/risk scores.

### Valid identity, abnormal behavior

A valid service account can still perform unusual actions such as bulk export, abnormal request volume, new endpoint access, or unusual destinations. Trustvian detects behavioral deviation even when authentication succeeds.

## 5. Behavioral Fingerprinting

Fingerprints should be generated for:

- Services
- APIs
- Users
- Service accounts
- AI agents
- Devices
- Workloads

Potential fingerprint features:

```text
HTTP routes
HTTP methods
Service dependencies
Database operations
External destinations
Tool usage
Request frequency
Latency characteristics
Error patterns
Operation sequences
Deployment environment
Identity/context attributes
```

## 6. Sequence Analysis

Order matters. Trustvian should support sequence-based anomaly detection.

**Status: foundation implemented, as of the in-progress `v0.6`
milestone** (see [`docs/ROADMAP.md` §
v0.6](docs/ROADMAP.md#v06--behavioral-detection-depth) for the current,
accurate state — this is not yet a release, only implementation
progress). `internal/anomaly` has two signals here, deliberately kept
distinct rather than collapsed into one: `transition_deviation`
answers the narrowest possible version of this section's goal — has
the immediately preceding action ever led to this one before, for this
actor (`E(n-1) -> E(n)`, seen vs. never seen, deterministic) — while
`transition_novelty` in this sense is that same "never seen before"
case, and `transition_rarity` is a separate, graded question that only
applies once a transition *has* been seen at least once: how common is
it, expressed as an empirical relative frequency (never a
"probability" — no Markov model, no smoothing), gated on a
minimum-support threshold so a tiny sample is never mistaken for
genuine rarity. The two never fire on the same transition — a
transition is either novel (never seen) or, if seen, has a rarity
reading; it is never both. See
[`docs/sequence-analysis.md`](docs/sequence-analysis.md) for the full
design, [ADR
0010](docs/adr/0010-bounded-process-local-sequence-state.md) for why
this needed no new public type or storage abstraction, and [ADR
0011](docs/adr/0011-transition-rarity-statistic-and-orientation.md) for
the rarity statistic's exact definition and orientation.

A third, higher-order pair — `ngram_deviation`/`ngram_rarity` — extends
the identical novelty/rarity split one step further back: given a
fixed 3-gram `A -> B -> C`, has this exact (grandparent, predecessor)
pair ever led to this destination before, and if so, how commonly?
This is genuinely new information the two pairwise signals above
cannot express on their own: `A -> B` and `B -> C` can each be
independently familiar while the complete sequence has never occurred.
A fixed 3-gram only — no configurable sequence length, no Markov model
— see [ADR 0012](docs/adr/0012-bounded-trigram-behavioral-context.md)
for the design and for why a 3-gram's statistical denominator needed
its own new bounded state, not a reuse of the pairwise case's.

A first-order Markov signal, `markov_surprisal`, also now exists — but
not as new evidence: before building it, `v0.6`'s own Markov task
answered a mandatory question (would this duplicate `transition_rarity`
above?) and found that the textbook Markov "surprisal" statistic,
`-log(P(B|A))`, is a strictly monotonic function of the identical
frequency `transition_rarity` already reads. `markov_surprisal` is
therefore an alternative, bounded severity curve over that same
evidence — not independently weighted alongside it — see [ADR
0013](docs/adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md)
for the full analysis.

Beyond the fixed 3-gram and first-order surprisal above, arbitrary-length
n-grams, a full Markov treatment (a transition matrix, smoothing, a
higher-order model, or Markov merged with the 3-gram context) remain
unscoped future work — see ADR 0012's own "Future extension" section
and ADR 0011's "Why Markov still waits" (revisited, not superseded, by
the first-order surprisal signal above) for what specifically remains
missing; graph-based analysis and ML-based sequence models remain
research only, and — per [`docs/ROADMAP.md`](docs/ROADMAP.md)'s
explicit product boundary — ML must never become a dependency of the
core detection path.

Potential future algorithms:

- N-gram behavioral models beyond the fixed 3-gram implemented
- Full/higher-order Markov models (transition matrix, smoothing)
- Graph-based analysis
- Sequence similarity
- Statistical deviation
- ML-based anomaly detection

## 7. Trust Model

Do not expose a single opaque score only. Keep the component signals explainable.

Example:

```text
Behavior Score       = 0.82
Anomaly Score        = 0.91
Context Risk         = 0.63
Identity Confidence  = 0.95
Trust Score          = 0.31
```

Possible decisions:

```text
ALLOW
OBSERVE_ONLY
ALERT
CHALLENGE
REQUIRE_APPROVAL
BLOCK
```

## 8. Mathematical / Signal Processing Layer

The engine may optionally experiment with signal-processing techniques when they improve detection quality:

- Frequency-domain analysis
- Fourier transforms
- Complex-valued representations
- Spectral features
- Periodicity detection
- Phase relationships
- Correlation analysis

Keep this layer modular and optional. Novel mathematics must never be used merely for novelty.

Suggested packages:

```text
internal/signal/
  fft/
  complex/
  spectral/
  correlation/
```

## 9. Repository Structure

As built (see [Current Implementation Status](#current-implementation-status)
above and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the
authoritative, current version of this diagram): no `pkg/` layer —
`internal/` is Go's own encapsulation mechanism, and only the root
package plus `event/` and, as of `v0.4.0`, `alert/` are public (see
[ADR 0007](docs/adr/0007-alert-package-is-public.md) for why `alert`
specifically breaks the "public only if a real external consumer needs
to construct one" precedent `event` set and `policy`/`Config` still
follow).

```text
trustvian/
├── trustvian.go, engine.go, options.go, result.go   # public API (root package)
├── event/                                              # public: Event, Actor, Operation, Target, Context
├── alert/                                              # public: Alert, Severity, Condition/Rule/Evaluate, Sink, WebhookSink
├── cmd/
│   └── trustvian/                                       # CLI
├── internal/
│   ├── features/
│   ├── fingerprint/
│   ├── baseline/
│   ├── store/
│   ├── anomaly/
│   ├── trust/
│   ├── policy/
│   └── otel/                                            # the only package depending on OpenTelemetry
├── examples/                                            # runnable, genuinely-external-module demos (implemented)
├── processor/                                           # standalone OTel Collector processor, separate module (implemented)
├── docs/
│   └── adr/
├── go.mod
├── LICENSE
├── README.md
└── Makefile
```

`internal/signal/{fft,complex,spectral}` (§8, the optional
signal-processing layer) and a dedicated `internal/sequence` package
(see [`docs/ROADMAP.md` §
v0.6](docs/ROADMAP.md#v06--behavioral-detection-depth)) remain future
work. `examples/` and `processor/` are implemented, not merely
planned — corrected from this section's original text, which predated
both; a `Dockerfile` and `CONTRIBUTING.md` remain planned (see
[`docs/ROADMAP.md` §
v0.8](docs/ROADMAP.md#v08--production-runtime--storage)/[v0.9](docs/ROADMAP.md#v09--operational-readiness)).

## 10. CLI

The CLI should be developer-friendly:

```bash
trustvian analyze trace.json
trustvian fingerprint service payment-service
trustvian baseline build
trustvian policy test
trustvian agent start
trustvian version
```

Example output:

```text
Trustvian Behavioral Analysis

Service: payment-service
Anomaly: 0.91
Trust:   0.32
Risk:    HIGH

Detected:
  ! Unexpected dependency: secrets-manager
  ! External destination not in baseline
  ! Sequence deviation: 3 events

Decision: BLOCK
```

## 11. Go SDK

Keep the public API small and composable.

Conceptual API:

```go
engine := trustvian.NewEngine(
    trustvian.WithBaseline(baseline),
)

result := engine.Analyze(event)

fmt.Println(result.TrustScore)
fmt.Println(result.Decision)
```

Prioritize:

- Small interfaces
- Low allocations where practical
- Clear ownership of data
- Context-aware APIs
- Deterministic behavior
- Testability

## 12. OpenTelemetry Collector Processor

This should be one of the primary open-source deliverables.

Example:

```yaml
receivers:
  otlp:
    protocols:
      grpc:
      http:

processors:
  trustvian:
    anomaly_threshold: 0.85
    trust_threshold: 0.40

exporters:
  otlp:
    endpoint: "otel-backend:4317"

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [trustvian]
      exporters: [otlp]
```

The processor should enrich telemetry and optionally emit security decisions.

## 13. Free / Open Source Edition

The OSS edition should provide real value — and, per
[`docs/ROADMAP.md` § The OSS / Enterprise product
boundary](docs/ROADMAP.md#the-oss--enterprise-product-boundary), the
*complete* behavioral-security vertical (detect, score, decide, alert,
integrate, run), not a deliberately incomplete preview of it:

- Core behavioral engine
- Feature extraction
- Fingerprinting
- Baseline engine
- Anomaly detection
- Trust scoring
- Policy engine
- Alert evaluation and generic-webhook notification delivery
  (implemented as of `v0.4.0` — see [§ 18](#18-alert--notification-system))
- Go SDK
- CLI
- OpenTelemetry integration
- OpenTelemetry Collector processor
- Local storage options
- Docker support
- Examples
- Documentation

A developer should be able to install Trustvian locally and analyze real telemetry without creating an account.

## 14. Enterprise Edition

### Trustvian Control

**Correction (see [`docs/ROADMAP.md` § The OSS / Enterprise product
boundary](docs/ROADMAP.md#the-oss--enterprise-product-boundary)):**
this section's original list named "Alerting" as a potential
Enterprise feature. That is no longer the product direction and was
never correct once alerting was scoped — Trustvian OSS ships alert
evaluation and generic-webhook notification delivery itself (see
[§ 18](#18-alert--notification-system) and `docs/ROADMAP.md`'s `v0.4`).
Enterprise governs and centralizes; it does not provide the
capability. The list below is corrected accordingly:

Potential enterprise features:

- Centralized management
- Multi-tenancy
- RBAC
- SSO / SAML / OIDC
- Central/organization-wide policy management
- Behavioral profiles
- Historical analysis
- Security investigations
- Audit logs
- Service inventory
- Agent inventory
- Trust-score dashboards
- Centralized alert governance (routing, escalation, alert history at
  scale — not alerting itself, which is OSS)
- SIEM integration
- Kafka integration
- High availability
- Horizontal scaling
- Enterprise support

## 15. Control Dashboard

Example:

```text
TRUSTVIAN CONTROL

Services
------------------------------------------------
payment-service          TRUST 94%
customer-service         TRUST 91%
order-service            TRUST 88%
support-agent            TRUST 43%   HIGH RISK

Recent Anomalies
------------------------------------------------
12:41  support-agent
       Unexpected tool sequence

12:38  payment-service
       Abnormal database access

12:21  api-gateway
       Behavioral deviation
```

An anomaly should link to its relevant OpenTelemetry trace and span so investigators can move from:

```text
Anomaly
  -> Why?
  -> Behavior difference
  -> Related trace
  -> Relevant span
  -> API / Tool / DB / External call
```

## 16. AI-Agent Roadmap

**`v0.7` — AI Agent Behavioral Security is in progress; its foundation
task is done:** [`docs/ROADMAP.md` §
v0.7](docs/ROADMAP.md#v07--ai-agent-behavioral-security)
([task 014](docs/tasks/014-ai-agent.md)) is the pre-`v1.0` OSS
milestone for this. **AI agents are behavioral actors analyzed by the
same Trustvian engine** — not a second product, not a second security
engine. Agent identity, tool calls, external
destinations/file/database/secret access, and human approval were
already representable through the generic `Event`/`Actor`/`Target`
model (`ActorTypeAIAgent`, `OperationCategoryTool`,
`TargetCategoryExternal`/`Database`); task 014 added the genuinely
missing correlation dimensions as optional `Context` fields —
`SessionID` (session/conversation grouping), `DelegatedFrom`
(single-hop agent-to-agent delegation), and `ApprovalStatus` (a
recorded human-approval fact, not a workflow engine) — and proved,
with integration tests through the real engine, that tool-*sequence*
analysis needs no agent-specific algorithm: [§ v0.6 — Behavioral
Detection Depth](docs/ROADMAP.md#v06--behavioral-detection-depth)'s
existing bounded 3-gram signal already detects a novel tool sequence
(e.g. a sensitive read immediately followed by an external post) even
when both individual steps are independently familiar. Agents remain
"another behavioral source" through the same pipeline:

```text
Event → Features → Fingerprint → Baseline → Anomaly → Trust → Policy → Decision
```

A field affects `Fingerprint`/behavioral identity only if it describes
*what the actor typically does* — session, delegation, and approval
context describe one specific event, not a stable behavioral
dimension, so none of them enter `Fingerprint` identity (see
[ADR 0014](docs/adr/0014-ai-agents-as-first-class-behavioral-actors.md)
for the full reasoning and the cardinality proof this distinction
protects against).

Trustvian understands, as of task 014:

- Agent identity (`Actor.ID` — distinct from model/provider metadata,
  which is not behavioral identity and carries no dedicated field)
- Agent sessions (`Context.SessionID`, context-only)
- Tool calls (`Operation`/`Target`, reused as-is)
- Tool sequences (`v0.6`'s existing sequence signals, proven to
  generalize with zero new code)
- External calls, file access, database access, secrets access
  (`Target.Category`, `Config.SensitiveTargetFloor`, reused as-is)
- Human approval (`Context.ApprovalStatus`, a recorded fact)
- Agent-to-agent communication and delegation (`Context.DelegatedFrom`,
  a single hop)

**Task 030 — Approval-Aware Policy Semantics — is also done.**
[ADR 0015](docs/adr/0015-approval-as-policy-evidence-not-behavioral-anomaly.md)
gave `ApprovalStatus` its first real consumer, entirely inside
`internal/policy`: a `Policy` rule can now require approval for a
given operation (`Unless: &Condition{ApprovalStatus: Approved}`),
with **Policy, never the event, deciding whether approval is
required** — an event self-declaring `ApprovalNotRequired` cannot
exempt itself from a configured requirement, and missing evidence
fails closed to `BLOCK`. This settled the question this section
previously left open:

- **Implemented:** approval-aware deterministic policy evaluation —
  "is this operation authorized," expressed as ordinary `Policy` data,
  provably independent of behavioral (`Anomaly`/`Trust`) scoring, and
  domain-generic (not coupled to `ActorTypeAIAgent`).
- **Not implemented, and not this project's job:** an approval
  workflow, an approval request/notification flow, approval
  persistence, a human-approval UI, or approval granting of any kind.
  `ApprovalStatus` also remains untrusted, self-reported evidence — no
  cryptographic verification of its provenance exists; Trustvian
  evaluates the evidence an integration supplies, it does not
  authenticate where that evidence came from.

**Task 031 — Delegation Behavioral Semantics — is also done.**
[ADR 0016](docs/adr/0016-delegation-as-behavioral-evidence-not-provenance.md)
gave `DelegatedFrom` its first real consumer: a new, opt-in
`internal/anomaly` signal (`delegation_deviation`) that flags a
delegator this actor has never received delegation from before,
learned via a small, bounded (64-entry) map on `Baseline`. Two
distinctions this section must state precisely, because they are easy
to get wrong:

- **Implemented:** bounded behavioral learning of immediate-delegator
  familiarity — "does this actor normally receive delegated work from
  this delegator?" — provably independent of `Fingerprint` identity,
  of approval-policy evidence (task 030), and of
  `Actor.IdentityConfidence`.
- **Not implemented, and not this project's job:** delegation
  authentication or authorization of any kind. A familiar delegator is
  never thereby authorized, and an unfamiliar one is never thereby
  malicious — `delegation_deviation` answers "is this unusual for this
  actor," never "is this delegation authentic." `DelegatedFrom` remains
  exactly as unauthenticated and self-reported as it was after task
  014; a malicious actor can still behaviorally "normalize" a forged
  delegator through repetition, and nothing in this project verifies
  the claim's provenance. No delegation graph, multi-hop chain, depth
  scoring, or delegation-rarity/Markov/n-gram signal exists either —
  the smallest meaningful behavioral evidence was the explicit target,
  not the ceiling.

**Task 032 — Agent Security Scenario Validation — is also done.** The
three capabilities above are now validated **in combination**, not
just individually: agent behavior, `v0.6` sequence analysis, delegation
deviation, and approval-aware policy compose correctly against five
representative scenarios (unexpected privileged tool, sensitive
read-then-external-post sequence, approval violation, unexpected
delegator, external-destination drift) plus one combined case — with
zero new detectors and proven independence from double-counting. This
task also surfaced, and documents rather than silently patches, a real
gap: `anomaly.Config` has no public-`config`-package equivalent of
`policy.Policy`'s `config.CompilePolicy` path (available since `v0.5`),
so an OSS consumer outside this module cannot enable
delegation/sequence signals through public API alone today — only
task 030's approval mechanism is demonstrable that way (see
[examples/ai-agent-security](examples/ai-agent-security/)).

Not yet built, and not any of these four tasks' job: delegation
provenance verification (signed claims, a trusted orchestration layer,
identity-provider evidence) and a public configuration surface for
`anomaly.Config` — both distinct, genuinely future work this project
has not designed, only identified.

**MCP.** Trustvian's read/query surface may eventually be exposed to AI
agents and developer tooling via MCP
([`docs/ROADMAP.md` §
Trustvian MCP](docs/ROADMAP.md#trustvian-mcp), [task
015](docs/tasks/015-trustvian-mcp.md)) — an adapter that wraps and
queries the existing public API, never the reverse:

```text
MCP -> Trustvian public API -> Trustvian Engine       (correct)
Trustvian Core -> MCP                                  (wrong)
```

MCP is an optional integration surface, not a core dependency and not
a release blocker for OSS `v1.0` — see [§ Product Evolution Toward
v1.0](#product-evolution-toward-v10) below.

## 17. Policy Engine

**Status: implemented, as of `v0.5.0` (not yet released — see
[`docs/ROADMAP.md`](docs/ROADMAP.md) § v0.5 for the current, accurate
state)** — a public `config` package (`PolicyConfig`/`PolicyRule`/
`PolicyCondition`, `Validate`, `CompilePolicy`, `Load`/`LoadFile`),
consumed identically by the Go SDK, the CLI (`--config`), and (in
code, pending that same release) the OTel Collector processor. §17.4's
"the same loader and file format may express both" illustrative
sketch was evaluated against the real, released schema v1 and
deliberately **not** taken: Alert configuration ended up as its own
separate document and type (`AlertConfig`/`CompileAlerts`), not a
`policies:`/`alerts:` combined schema — see [ADR
0009](docs/adr/0009-alert-config-is-a-separate-document.md) for why.
See [Current Implementation Status](#current-implementation-status)
above for the caveat this whole document carries.

`internal/policy.Policy.Evaluate` already exists and works today: an
ordered `[]Rule`, first-match-wins, fail-closed to `BLOCK` on
misconfiguration (see [`docs/policy-guide.md`](docs/policy-guide.md)).
What's missing is a way to *configure* that engine without writing Go
code inside this module — the problem [`docs/ROADMAP.md` §
v0.5](docs/ROADMAP.md#v05--policy--configuration) exists to close.

### 17.1 The problem

`Anomaly → Trust → Policy → Decision` already works; a `Policy` value
just can't be *constructed* by anything outside this module today
([ADR 0002](docs/adr/0002-public-api-boundary.md)). Concretely, this is
why `processor/` (the standalone OTel Collector processor) resolves
every span to `trustvian.decision = "observe_only"` regardless of how
anomalous the underlying behavior is — it runs Trustvian's
zero-configuration default `Policy` because it has no way to supply its
own (see [`processor/README.md` §
Configuration](processor/README.md#configuration)). A CLI flag, a
config file, and a Collector-pipeline setting should all be able to
express the same policy, but none of them can today.

### 17.2 Architectural goal

> Users should be able to configure meaningful Trustvian policy
> behavior without writing custom Go code or depending on internal
> packages.

```text
Configuration
      ↓
Validation
      ↓
Public Policy Configuration
      ↓
Internal Policy Compilation
      ↓
Engine
```

Avoid, deliberately:

```text
External Consumer
      ↓
internal/policy
```

`v0.5`'s job is a **narrow, stable public configuration boundary** —
the minimum types an external loader needs to *construct* a `Policy`
(and, since it converges on the same semantics, an `alert.Rule` set —
see [§ 17.4](#174-relationship-to-alert-configuration)) — not
`internal/policy` exposed wholesale. This is the same resolution
[ADR 0007](docs/adr/0007-alert-package-is-public.md) already applied
to `alert`: promote exactly what a real external consumer needs to
construct, nothing more.

The intended consumers all converge on the same semantics:

```text
Go SDK
CLI
OTel Collector Processor
Standalone Runtime
```

### 17.3 Declarative configuration direction

**Illustrative only — semantics before syntax; this is not a
committed DSL, and no parser exists yet.** The example below matches
`internal/policy.Condition`'s already-implemented flat,
AND-of-optional-fields shape (see [§ 18.4](#184-alert-rules) for the
identically-shaped alert-rule illustration) — it does not introduce
combinators, expressions, or scripting that engine doesn't already
support:

```yaml
policies:
  - name: suspicious-secret-access
    when:
      anomaly_score: "> 0.80"
      target_category: secret
    decision: require_approval
```

```yaml
alerts:
  - name: critical-risk
    when:
      risk: critical
    notify:
      - security-webhook
```

### 17.4 Relationship to alert configuration

Policy configuration and alert configuration are related — the same
loader and file format may express both — but they remain separate
runtime concepts, exactly as [§ 18.1](#181-why-alert-is-not-decision)
already establishes for `Policy`/`Decision` versus `Alert Evaluation`:

```text
Policy
  ↓
Decision
```

```text
Decision / Result
       ↓
Alert Evaluation
```

`v0.5` does not merge alert rules into the core `Policy` domain — a
configuration file may configure both `policies:` and `alerts:`
sections, but the two are compiled into `Policy` and `[]alert.Rule`
independently, preserving `alert.Evaluate`'s existing structural
independence from `internal/policy` (see [§
18.4](#184-alert-rules)).

### 17.5 Configuration principles

- **Single semantic model.** The Go SDK, CLI, Collector processor, and
  any future standalone runtime must not develop separate policy
  semantics — one file format, one loader, one compiled `Policy` shape,
  consumed identically everywhere.
- **Validation.** Invalid configuration must fail predictably with a
  useful error — the same fail-closed discipline
  `policy.Policy.Evaluate` already applies to a misconfigured `Policy`
  at runtime, applied here at load time instead.
- **Backward compatibility.** Future configuration evolution must
  consider versioning from its first release — the same "no silent
  reinterpretation" discipline `internal/fingerprint`'s versioned hash
  and `alert.Envelope`'s `PayloadVersion` already established for this
  codebase.
- **Determinism.** The same valid configuration and the same
  behavioral `Result` must always produce the same policy decision.
- **Security.** Configuration is security-sensitive input — it decides
  `ALLOW`/`BLOCK`/`REQUIRE_APPROVAL` — and must be validated
  defensively, not trusted as an operator-controlled convenience.
- **Narrow public API.** Do not expose internal implementation merely
  for convenience — promote only what an external loader must
  construct, per [§ 17.2](#172-architectural-goal).

### 17.6 Example (existing, illustrative)

```yaml
policy:
  name: prevent-unexpected-agent-tools

  when:
    actor.type: ai_agent
    tool.category: secrets

  unless:
    approval: human

  action:
    block: true
```

Supported actions should include:

```text
ALLOW
OBSERVE
ALERT
CHALLENGE
REQUIRE_APPROVAL
BLOCK
```

## 18. Alert & Notification System

**Status: the Foundation stage (§ 18.1–18.10) is implemented, as of
`v0.4.0` — a public `alert` package (`Alert`, `Severity`,
`Condition`/`Rule`/`Evaluate`, `Sink`, `WebhookSink`).** The remaining
stages this section describes (§ 18.11 delivery reliability, § 18.12
deduplication/cooldown, provider-specific sinks beyond the generic
webhook) are still architectural target, not implemented. See
[`docs/ROADMAP.md`](docs/ROADMAP.md) for what's actually scheduled and
in what order, and [Current Implementation Status](#current-implementation-status)
above for the same caveat this whole document carries.

### 18.1 Why Alert is not Decision

Trustvian's existing pipeline —

```text
Event
 -> Features
 -> Fingerprint
 -> Baseline
 -> Anomaly
 -> Trust
 -> Policy
 -> Decision
```

— answers one question: **what should Trustvian do about this
behavior** (`ALLOW`, `OBSERVE_ONLY`, `ALERT`, `CHALLENGE`,
`REQUIRE_APPROVAL`, `BLOCK`). This pipeline is not being redesigned,
extended with a new stage, or reordered by anything in this section.

Alerting answers a different question: **should this security event be
communicated to an external system** (a webhook, a chat channel, a
paging system)? A `Decision` of `BLOCK` at `RiskHigh` might well
produce an alert; a `Decision` of `OBSERVE_ONLY` on a routine,
low-severity event usually should not. The two concepts are related
but distinct. Conflating them would either force every `Decision` to
carry notification-delivery concerns it has no business knowing about,
or force every notification integration to reimplement policy
evaluation. Neither is acceptable.

Alerting is a layer that consumes the pipeline's output without
becoming part of it:

```text
Event
 -> Features
 -> Fingerprint
 -> Baseline
 -> Anomaly
 -> Trust
 -> Policy
 -> Decision
 -> Alert Evaluation
 -> Alert
 -> Notification Dispatcher
 -> Alert Sink(s)
```

```text
Decision
   |
   v
Alert Evaluation
   |
   v
 Alert
   |
   v
Notification Dispatcher
   |
   +----------+----------+----------+-------------+
   |          |          |          |             |
Webhook     Slack      Teams    PagerDuty      Other
```

Not every `Decision` produces an `Alert`; an `Alert` never changes what
a `Decision` already was. Alert Evaluation reads `Result` (or whatever
its future stable equivalent is); it never writes back into the
pipeline, and nothing upstream of `Decision` ever needs to know
alerting exists.

### 18.2 Alert domain concept

**Implemented as of `v0.4.0`**, as `alert.Alert` — see
[DOMAIN.md § Alert](docs/DOMAIN.md#alert) for the exact, current field
list. An `Alert` is the notification-worthy summary of one behavioral
decision. Its domain semantics — described here as concepts this
section originally fixed ahead of implementation, now realized in the
real Go type — are:

- an alert identifier
- a timestamp
- a severity (see [§18.3](#183-alert-severity))
- the decision that produced it
- the risk level
- the actor
- the target
- the trust score
- the anomaly score
- the reasons / explanation for the decision
- the fingerprint identifier
- open metadata for extension

**Not carried into the implementation:** a "behavior identifier"
(the actor's behavioral history this event belongs to) — this list
originally named one, but every plausible meaning collapses into
`Fingerprint.ID`, exactly the same finding [task
008](docs/tasks/008-otel.md) already reached for
`trustvian.behavior.id` in the OTel attribute set (see
[OPENTELEMETRY.md](docs/OPENTELEMETRY.md#trustvian-output-attributes)).
`Alert.FingerprintID` is the one identifier that concept resolves to;
no second, distinct behavior-level identity exists anywhere in this
codebase.

Deliberately absent from this list: a second copy of trust score,
anomaly score, risk, decision, actor, target, or explanation logic.
Every one of those concepts already exists — `Trust.Score`,
`Anomaly.Score`, `Trust.Risk`, `Decision`, `Event.Actor`,
`Event.Target`, and `Result.Explain()`/`Anomaly.Contributors` today.
`Alert` is a view assembled from an existing `Result`, not a parallel
model that could drift from it.

### 18.3 Alert severity

**Implemented as of `v0.4.0`**, as `alert.Severity`. `Alert` introduces
one genuinely new concept: **severity**:

```text
INFO
LOW
MEDIUM
HIGH
CRITICAL
```

Severity answers "how loudly should this be communicated," which is a
distinct question from:

- **anomaly score** — how different this event looks from the baseline
- **trust score** — how much this event should be trusted, combining
  anomaly, identity, and context
- **risk level** — `Trust`'s own qualitative bucket
- **decision** — what the policy engine chose to do

These four already exist and are not being redefined. Severity is not
assumed to map one-to-one onto any of them (a `BLOCK` at `RiskCritical`
against a known-sensitive target might always be `CRITICAL` severity by
an operator's own rule; a `CHALLENGE` at `RiskMedium` on a first-time
integration might reasonably be `INFO`). Alert Evaluation — not
`Decision` — is responsible for turning risk/decision/anomaly into a
severity, using operator-defined rules (§18.4); no built-in mapping
ships, matching the implementation exactly.

### 18.4 Alert rules

**The Go-level matching mechanism is implemented as of `v0.4.0`**
(`alert.Condition`/`alert.Rule`/`alert.Evaluate` — first-match-wins,
flat AND-of-optional-fields), **and the declarative YAML surface is
now implemented too, as of `v0.5.0`** (not yet released — see
[`docs/ROADMAP.md`](docs/ROADMAP.md) § v0.5): `config.AlertConfig`/
`AlertRuleConfig`/`AlertConditionConfig`, `CompileAlerts`,
`LoadAlerts`/`LoadAlertsFile`. It matches on the exact six concepts
listed just below, no more — `severity` is not matchable, exactly as
this section already specified, since it's the rule's *output*, not an
input. It does not use this section's illustrative `alerts:`
list-of-`when:` shape or its `">0.90"` comparison-operator-in-string
syntax verbatim (see the real, current shape in [ADR
0009](docs/adr/0009-alert-config-is-a-separate-document.md) and
[`docs/tasks/023`](docs/tasks/023-declarative-alert-configuration.md)):
numeric thresholds are typed `float64` fields
(`min_anomaly_score`/`max_trust_score`), not an embedded operator
string, matching `PolicyCondition`'s own established convention of
typed, validated fields over a mini-expression-language. Not yet wired
into the CLI or the Collector processor — a Go SDK caller can use it
directly today.

Alert Evaluation is driven by rules an operator configures, matching on
concepts Trustvian already exposes — implemented today as
`decision`, `risk level` (as a floor), `actor type`, `target category`,
`anomaly score` (as a floor), and `trust score` (as a ceiling);
`severity` is not itself a matchable input (it's the *output* a
matched `Rule` assigns, per [§18.3](#183-alert-severity)):

- decision
- risk level
- actor type
- target category
- anomaly score
- trust score

Illustrative only — not a contract, no YAML parser exists yet:

```yaml
alerts:
  - name: critical-security-event
    when:
      severity: critical
    actions:
      - webhook

  - name: blocked-sensitive-operation
    when:
      decision: block
      target_category: sensitive
    actions:
      - webhook

  - name: high-anomaly
    when:
      anomaly_score: ">0.90"
    actions:
      - webhook
```

The initial architecture supports simple, flat matching on these
existing concepts — the same shape `internal/policy.Condition` already
uses for `Decision` evaluation (AND-of-optional-fields, no
combinators). It does **not** introduce boolean combinators, a general
expression language, or dynamic rule loading in its first version;
those are explicitly future evolution, not a v1 requirement, following
the same "don't build the complex policy language yet" discipline
`docs/policy-guide.md` already documents for `Condition` itself.

### 18.5 Notification Dispatcher

**Not implemented as a distinct component in `v0.4.0`.** Today, a
caller that gets `(Alert, true)` back from `alert.Evaluate` calls a
`Sink`'s `Send` directly — there is exactly one `Sink` per call site,
no named-actions matching, and no fan-out to multiple sinks from one
`Alert`. A real "named actions, dispatch to all matching sinks"
component, as described below, is future work — most naturally
alongside `v0.5`'s configuration format (an `alerts:` rule's
`notify:` list is exactly this dispatcher's input) or the
provider-sinks/governance stage named in
[`docs/ROADMAP.md`](docs/ROADMAP.md#alert--notification-phase).

The Notification Dispatcher's one job, once built:

```text
Alert
  -> determine which configured notification actions match
  -> dispatch to each matching Alert Sink
```

It contains no provider-specific logic. It does not know what a Slack
message looks like, what a webhook's HTTP method is, or how PagerDuty's
API is shaped — it only knows "this alert matched these named actions,
dispatch to them."

```text
Alert
  |
  v
Notification Dispatcher
  +-- Webhook Sink
  +-- Slack Sink
  +-- Teams Sink
  +-- PagerDuty Sink
  +-- Custom Sink
```

### 18.6 Alert Sink abstraction

**Implemented as of `v0.4.0`**, as `alert.Sink` — named `Sink`, not
`AlertSink`, to avoid the package/type stutter (`alert.AlertSink`)
this codebase generally avoids for secondary types; the signature
below matches exactly:

```go
type Sink interface {
    Send(ctx context.Context, alert Alert) error
}
```

It exists so a new provider can be added by implementing one method
against a stable `Alert` shape, without anything upstream of it
changing (there is no Notification Dispatcher yet to keep unchanged —
see [§18.5](#185-notification-dispatcher)). Where this interface lives
was a real, deliberately-surfaced open question at the `v0.4.0` task's
own planning stage — resolved as *public*, not `internal/`, and
recorded in [ADR 0007](docs/adr/0007-alert-package-is-public.md): a
third-party `Sink` implementation is the entire reason this interface
exists, which Go's `internal/` visibility rule would make impossible
if `Alert`/`Sink` lived under `internal/`.

### 18.7 Generic webhook is the primary integration

**Implemented as of `v0.4.0`**, as `alert.WebhookSink`. The first, and
most important, notification target is a **generic HTTP webhook** —
provider-neutral, and the integration point every other target (n8n, a
SIEM, a SOAR platform, a ticketing system, a custom internal API) can
build on without Trustvian knowing any of them exist:

```text
Trustvian -> Alert -> Sink (WebhookSink today) -> External system
```

(No Notification Dispatcher exists yet to sit between `Alert` and
`Sink` — see [§18.5](#185-notification-dispatcher).)

Slack and Microsoft Teams are not hard-coded into the core engine, and
were not the first integration built — they remain two of many things
that could eventually sit behind the same `Sink` abstraction the
webhook itself uses.

### 18.8 Slack / Microsoft Teams

Still documented here as **future** notification adapters, behind the
same `Sink` abstraction as the webhook — not as Trustvian Core
dependencies. The generic webhook (§18.7) shipped first, as planned;
Slack and Teams (and PagerDuty, and anything else) remain unbuilt —
see [`docs/ROADMAP.md` § Alert & Notification
phase](docs/ROADMAP.md#alert--notification-phase), which also corrects
this document's own earlier framing: a provider-specific `Sink` is OSS
scope if architecturally clean, same as the webhook, not automatically
Enterprise territory. Trustvian Core must never import a Slack or Teams
SDK to support this.

### 18.9 Webhook security

**Implemented as of `v0.4.0`.** `alert.WebhookSink` enforces, at
construction or on every `Send`:

- HTTPS only (`NewWebhookSink` rejects a non-`https://` destination)
- HMAC-SHA256 request signing, computed over
  `"<unix-timestamp>.<payload-body>"` — not the body alone, so a
  receiver can enforce a replay window without an attacker being able
  to attach a fresh timestamp to a previously-valid signature
- a delivery timestamp header (`X-Trustvian-Timestamp`)
- a distinct delivery identifier (`X-Trustvian-Delivery-ID`), separate
  from the alert's own `X-Trustvian-Alert-ID`
- a bounded request timeout (default 5s, configurable)
- a bounded payload size (rejects an oversized payload before ever
  making a network call)
- a basic literal-IP loopback/link-local destination check (not a full
  DNS-resolution-based SSRF defense — see
  [`docs/SECURITY.md` § Alert/notification delivery
  integrity](docs/SECURITY.md#alertnotification-delivery-integrity)
  for the documented limitation)

Real request headers, matching exactly:

```http
X-Trustvian-Signature: sha256=...
X-Trustvian-Timestamp: ...
X-Trustvian-Alert-ID: ...
X-Trustvian-Delivery-ID: ...
```

**Not yet addressed:** authentication for whoever *configures* the
destination (a config-time concern, not a delivery-time one — belongs
with [§ 17](#17-policy-engine)/`v0.5`'s configuration validation), and
full DNS-based SSRF protection (explicitly out of scope — Trustvian is
not a network-egress enforcement point, the same boundary
`docs/SECURITY.md`'s introduction already draws for transport-layer
concerns generally).

### 18.10 Stable, versioned webhook payload

**Implemented as of `v0.4.0`**, as `alert.Envelope{Version, Alert}`
with `alert.PayloadVersion = "1"` — a **versioned contract** from its
first release, not an internal struct serialized as a convenience.
Real shape, sent by `WebhookSink.Send` today:

```json
{
  "version": "1",
  "alert": {
    "id": "alt_...",
    "timestamp": "2026-09-07T09:42:12Z",
    "severity": "critical",
    "decision": "block",
    "risk": "high",
    "trust_score": 0.31,
    "anomaly_score": 0.94,
    "actor": {
      "id": "payment-service",
      "type": "service"
    },
    "target": {
      "name": "/customers/export",
      "category": "external"
    },
    "fingerprint_id": "fp_...",
    "reasons": [
      "Previously unseen operation",
      "Abnormal request frequency"
    ]
  }
}
```

Differences from this section's original illustrative shape, worth
naming explicitly rather than silently replacing: the envelope wraps
`alert` as a nested object rather than flattening every field
top-level (so `version` reads unambiguously as the *envelope's*
version, not the alert's); there is no `event: "trustvian.alert"`
marker field (redundant once the payload only ever carries one shape);
`target.id` is `target.name` (matching `event.Target`'s own field
name, reused directly rather than renamed); and there is no
`behavior_id` field — see [§18.2](#182-alert-domain-concept) for why
that concept was dropped rather than implemented. An optional
`metadata` object is also present when the `Alert` carries any (open
extension data, omitted when empty).

### 18.11 Delivery reliability

A future delivery layer needs, at minimum: a timeout, retry with
exponential backoff, a maximum retry count, a delivery status, explicit
handling of a permanently-failed delivery (a dead-letter/failure
state), idempotency, and a delivery identifier distinct from the
alert's own identifier.

```text
Alert -> Delivery -> Webhook -> 503 -> Retry -> Retry -> Success
```

An `Alert` is independent of any one delivery attempt — the same alert
can have multiple delivery attempts, across multiple sinks, each with
its own independent outcome:

```text
Alert
  +-- Delivery #1 -> Webhook   -> Success
  +-- Delivery #2 -> Slack     -> Failed
  +-- Delivery #3 -> Teams     -> Success
```

None of this is implemented today.

### 18.12 Deduplication and cooldown

A single behavioral incident can produce many qualifying events —
`payment-service` calling `POST /customers/export` at `CRITICAL`
severity a hundred times in a minute must not become a hundred
identical alerts. The architecture calls for, eventually: an alert
fingerprint/key distinct from the behavioral `Fingerprint.ID`,
deduplication against that key, a cooldown window, suppression, and
escalation when suppression itself becomes suspicious. Keep the first
version of this simple; a full incident-management system is
explicitly out of scope — see §18.13.

### 18.13 Alert vs. Incident

For the current roadmap: **an Alert is one notification-worthy security
event.** An Incident — a higher-level concept that groups multiple
related alerts into one investigation — may be a future evolution, but
is not designed, scoped, or implied by anything in this section.
Trustvian is not introducing an incident-management domain now.

### 18.14 Relationship to OpenTelemetry

The Alert/Notification system does not change Trustvian's OTel
boundary. OTel remains strictly an *input* integration:

```text
Application / Agent -> OTel -> Trustvian OTel Adapter / Collector Processor -> Trustvian Engine
```

Alerting is downstream of the engine's own output, entirely separate
from how events arrived:

```text
Trustvian Engine -> Decision -> Alert -> Notification Dispatcher
```

The Alert system must never depend on OpenTelemetry, and notification
delivery must work identically whether or not the deployment uses OTel
at all — exactly the same independence `internal/otel` already
maintains from the core engine (see
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)).

### 18.15 Core architecture constraint

This constraint is as important as anything else in this section: none
of the following may become a dependency of Trustvian's **core
detection engine** (`event`, `internal/features` through
`internal/policy`, and root `Engine`) merely to support alerting —

```text
Slack SDK · Teams SDK · an HTTP client · Kafka · Redis · PostgreSQL · a PagerDuty SDK
```

Trustvian Core stays:

```text
Trustvian Core -> domain logic -> decision
```

with every external notification integration living behind the `Sink`
boundary (§18.6), the same philosophy `internal/otel` already enforces
for OpenTelemetry — see
[`docs/ARCHITECTURE.md` § package boundaries](docs/ARCHITECTURE.md#package-boundaries).
**Verified as implemented**, not just argued: `go list -deps` on
`event` through `internal/policy` and the root package shows no
`net/http`, no `alert` import, and no provider SDK — `alert` (which
does import `net/http`, in `WebhookSink`) is deliberately *not* part of
the core detection engine this constraint describes; it is the
downstream layer the constraint protects the core *from*, per
[§18.1](#181-why-alert-is-not-decision).

### 18.16 OSS / Enterprise boundary

Evolutionary and adoption-driven, not fixed today. The OSS core should
carry enough of this to be genuinely useful standalone — the first
three items below are implemented as of `v0.4.0`, not merely intended:

- the `Alert` domain model — done
- basic alert evaluation — done (`alert.Condition`/`Rule`/`Evaluate`)
- a generic webhook sink — done (`alert.WebhookSink`)
- the base notification-dispatch abstraction — not yet built (see
  [§18.5](#185-notification-dispatcher))
- local, file/config-based rule configuration — not yet built (see
  [§17](#17-policy-engine)/`v0.5`)

Trustvian Control/Enterprise is where centralization and governance
belong once they're needed:

- centralized alert management
- advanced alert rules (combinators, richer expressions)
- multi-tenant notification configuration
- notification routing and escalation
- suppression policy management
- alert history
- delivery observability
- RBAC over alert configuration
- audit of who changed what alert rule
- centralized/managed Slack, Teams, PagerDuty integrations
- enterprise notification governance

No feature above is locked into Enterprise as a permanent,
non-negotiable line — this list reflects where things would sensibly
start, informed by real adoption, matching how [§13](#13-free--open-source-edition)
and [§14](#14-enterprise-edition) already draw this line for the rest
of the product.

### 18.17 Package direction — decided, not illustrative, for `alert` itself

**Superseded by the actual `v0.4.0` decision.** This section originally
sketched `internal/alert/` as a plausible, non-committal future
location. That guess was wrong in exactly the way its own reasoning
should have ruled out: `alert.Sink`'s entire purpose is for third-party
code to implement it against a stable `Alert` type, which Go's
`internal/` visibility rule makes structurally impossible. `alert`
therefore lives as a **public** top-level package
(`github.com/Trustvian/trustvian/alert`, a sibling to `event`), not
under `internal/` — decided deliberately and recorded in
[ADR 0007](docs/adr/0007-alert-package-is-public.md), following the
exact "prove the interface is needed, then place it" discipline
[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) applies to every package
boundary in this codebase — this was not a default reached by habit.

What remains genuinely undecided, illustrative only: where a future
provider-specific sink (Slack, Teams) or the Notification Dispatcher
(§18.5) would live. A plausible, still non-committal shape:

```text
alert/                  (public — Alert, Severity, Condition/Rule/Evaluate, Sink, WebhookSink)
internal/notification/
    slack/
    teams/
```

`internal/notification/{slack,teams}` is a reasonable home for
provider-specific sink *implementations* specifically because — unlike
`alert.Sink` itself — a Slack/Teams sink's own types (its HTTP client
config, its message-formatting logic) have no third-party-construction
requirement; the "internal by default, public is deliberate" reasoning
ADR 0007 applied to `alert` doesn't create the same tension there. This
is still not created now, and this exact structure is still not a
commitment.

## 19. Roadmap

[`docs/ROADMAP.md`](docs/ROADMAP.md) is the authoritative, current
roadmap — organized by shippable milestone
(`v0.1`→`v0.2`→`v0.3`→`v0.4.0`, shipped; `v0.5`→`v0.9`, planned;
`v1.0`, the production-readiness gate), reconciled against what's
actually implemented today, with a detailed, independently-scoped task
breakdown under [`docs/tasks/`](docs/tasks/). This section does not
duplicate that task-level detail — it explains *why* each milestone
exists and how it fits the product's architecture; see
`docs/ROADMAP.md` for exact scope, dependencies, and acceptance
criteria per milestone.

### Product Evolution Toward v1.0

```text
v0.1  Core Behavioral Engine            SHIPPED
        ↓
v0.2  OpenTelemetry                     SHIPPED
        ↓
v0.3  Baseline & Anomaly Depth          SHIPPED
        ↓
v0.4  Alert & Notification Foundation   SHIPPED (v0.4.0)
        ↓
v0.5  Policy & Configuration            SHIPPED (v0.5.0)
        ↓
v0.6  Behavioral Detection Depth        SHIPPED (v0.6.0)
        ↓
v0.7  AI Agent Behavioral Security      IN PROGRESS (foundation task 014 done)
        ↓
v0.8  Production Runtime & Storage      PLANNED
        ↓
v0.9  Operational Readiness             PLANNED
        ↓
v1.0  Production-Ready OSS              TARGET — release gate, not a version bump
```

Why each step exists, not merely what it contains:

- **`v0.1`–`v0.3`** built and hardened the deterministic core this
  entire document assumes: `Event → Features → Fingerprint → Baseline
  → Anomaly → Trust → Policy → Decision`, plus the OpenTelemetry input
  path and the first depth signal (hour-of-day). Nothing downstream of
  this milestone changes that pipeline's shape.
- **`v0.4`** answered a question the pipeline structurally cannot
  answer itself: not "what should Trustvian do" but "should this be
  communicated externally" (§18.1). It had to come after `v0.1`
  specifically because Alert Evaluation reads a stable `Result`
  shape — a still-moving `Result` would mean redesigning the `Alert`
  view underneath it.
- **`v0.5`** exists because everything built so far is only
  configurable by writing Go code inside this module
  ([ADR 0002](docs/adr/0002-public-api-boundary.md)) — a real
  adoption blocker for a "standalone, production-usable" product. It
  is sequenced right after `v0.4` because it configures *both* `Policy`
  and `alert.Rule`, and both must already be stable to design a single
  config format against them (§17).
- **`v0.6`** exists because the current anomaly signals all score one
  event in isolation — order is information no per-event signal can
  see. It is independent of `v0.5`/`v0.7`, sequenced here because
  sequence-aware detection is itself a prerequisite a production
  behavioral-security product needs before `v1.0`, not because
  anything else structurally depends on it first.
- **`v0.7`** exists because AI agents are already a first-class actor
  type but lacked three correlation dimensions (session, delegation,
  approval) that a real agent deployment needs — and because this is
  where this document repeatedly insists the boundary matters most:
  reuse the existing engine, never build a second one. Task 014 closed
  that representational gap; further `v0.7` slices (approval-aware
  policy semantics, delegation behavioral semantics) are scoped, not
  yet implemented — see `docs/ROADMAP.md` § v0.7.
- **`v0.8`–`v0.9`** exist because a complete OSS product is not just a
  correct algorithm — it is something an operator can actually run,
  persist state for, upgrade, and trust operationally.
- **`v1.0`** is the point all of the above adds up to: a release gate,
  not a new capability, and not a claim this document makes today —
  see [§ OSS v1.0 — Production-Ready Definition](#oss-v10--production-ready-definition)
  below.

### OSS v1.0 — Production-Ready Definition

> A user can install Trustvian, feed behavioral telemetry into it,
> build baselines, detect anomalous behavior, calculate trust/risk,
> apply policies, produce decisions, generate alerts, integrate
> notifications, persist state, configure the system, observe it,
> upgrade it, and operate it independently in production — without
> requiring Trustvian Control.

```text
Observe → Model → Detect → Score → Decide → Alert → Integrate → Operate
```

This is the OSS `v1.0` product contract — not a claim that it is met
today. As of this document's writing, `Observe` through `Alert` are
real (`v0.1`–`v0.4.0`, shipped); `Integrate` is partially real (Go SDK,
CLI, OTel adapter, Collector processor all ship today; MCP remains
optional and does not gate this contract — see
[§16](#16-ai-agent-roadmap)); `Operate` (production persistence,
deployment packaging, self-observability, release engineering) is the
`v0.8`/`v0.9` work still ahead. See
[`docs/ROADMAP.md` §
v1.0](docs/ROADMAP.md#v10--production-ready-oss) for the actual
release-gate themes (correctness, performance, security, operations,
documentation) this contract resolves into when the milestone is
evaluated.

### Historical framing (superseded)

The phases below are this document's original, long-term framing,
predating the milestone sequence above. Retained for history; where
the two differ, the sequence above (and `docs/ROADMAP.md`) reflects
the real, current plan — not this section.

### Phase 0 — Foundation

- Go module
- Repository
- License
- CI/CD
- Documentation
- Core domain models
- Unit tests
- Benchmark framework

### Phase 1 — Behavioral Engine

- Event model
- Feature extraction
- Behavioral fingerprint
- Baseline
- Similarity calculation
- Anomaly score
- Trust score
- Explainable decisions

### Phase 2 — OpenTelemetry

- OTLP ingestion
- Trace processing
- OTel attributes
- OTel Collector processor
- Example Collector deployment
- Trace-to-security correlation

### Phase 3 — Policy Engine

- Policy model
- Rule evaluation
- ALLOW / BLOCK / CHALLENGE
- Policy testing CLI
- Policy versioning

### Phase 4 — AI Agent Security

- Agent identity
- Tool-call tracking
- Tool sequence analysis
- Agent behavioral baseline
- Agent trust score
- Human approval workflows

### Phase 5 — Open Source Platform

- CLI improvements
- Docker image
- Helm chart
- Kubernetes support
- Examples
- Documentation
- Community contribution model

### Phase 6 — Trustvian Control

- Web dashboard
- Central API
- Service inventory
- Behavioral profiles
- Anomaly investigation
- Historical analytics

### Phase 7 — Enterprise

- Multi-tenancy
- RBAC
- SSO
- Audit
- SIEM integrations
- Kafka
- HA
- Horizontal scaling
- Advanced policies
- Advanced analytics
- Enterprise support

## 20. Business Model

### Free

Target:

- Individual developers
- Open-source projects
- Small teams
- Researchers
- AI-agent developers

Include:

```text
Trustvian Engine
Go SDK
OTel Processor
CLI
Local deployment
Basic dashboard
Basic policies
Alerting & notification (webhook)
```

### Enterprise

Target:

- Banks
- Large enterprises
- SaaS companies
- Cloud platforms
- AI-agent platforms

Paid capabilities:

```text
Trustvian Control
Centralized governance
Multi-tenancy
SSO
RBAC
Audit
Advanced policies
Enterprise integrations
HA
Advanced analytics
Support
```

## 21. Differentiation

Trustvian is not:

- Another SIEM
- Another APM
- Another tracing system
- Another IAM
- Another generic anomaly detector

Positioning:

> **A behavioral security layer built on top of runtime telemetry.**

The conceptual distinction:

```text
IAM
  = Who are you?

Observability
  = What happened?

Trustvian
  = Should this behavior be trusted?
```

## 22. Brand Architecture

```text
Trustvian
│
├── Trustvian Engine
│   └── Open-source behavioral security engine
│
├── Trustvian OTel
│   └── OpenTelemetry integrations
│
├── Trustvian Agent
│   └── Runtime / AI-agent integration
│
├── Trustvian Policy
│   └── Behavioral security policy engine
│
├── Trustvian Control
│   └── Enterprise management dashboard
│
└── Trustvian Cloud
    └── Managed enterprise offering
```

## 23. Brand Messaging

**Primary:**  
> Trust the Behavior.

**Secondary:**  
> From Behavior to Trust.

**Enterprise:**  
> Behavioral Security for Modern Systems.

**AI Agents:**  
> Don't just authenticate your agents. Trust their behavior.

**OpenTelemetry:**  
> Turn telemetry into behavioral security signals.

## 24. Claude Code Implementation Instructions

Build Trustvian as a production-quality open-source Go project.

Priorities:

1. Clean architecture
2. Small interfaces
3. Testability
4. Low runtime overhead
5. Deterministic behavior
6. Explainable security decisions
7. OpenTelemetry compatibility
8. Extensibility
9. Backward compatibility
10. Clear documentation

Do not build a fake enterprise dashboard before the core engine works.

Build vertically:

```text
Event
  -> Features
  -> Fingerprint
  -> Baseline
  -> Anomaly
  -> Trust
  -> Policy
  -> Decision
```

Every major component must have:

- Unit tests
- Benchmarks where performance matters
- Clear interfaces
- Example usage
- Documentation

## 25. Initial MVP Acceptance Criteria

The first usable release must support:

```text
Input:
  OpenTelemetry trace/event

Processing:
  Feature extraction
  Behavioral fingerprint
  Baseline comparison
  Anomaly score
  Trust score

Output:
  Risk
  Decision
  Explanation
```

The MVP must work locally with no external SaaS dependency.

## 26. Long-Term Vision

Trustvian should evolve from an open-source Go engine into a behavioral security platform and eventually a behavioral trust layer for applications and AI agents.

```text
                    TRUSTVIAN
                         |
        +----------------+----------------+
        |                |                |
   Applications      AI Agents       Services
        |                |                |
        +----------------+----------------+
                         |
                  OpenTelemetry
                         |
                         v
               Behavioral Intelligence
                         |
        +----------------+----------------+
        |                |                |
     Behavior          Risk            Context
        |                |                |
        +----------------+----------------+
                         |
                    Trust Score
                         |
                    Policy Engine
                         |
              +----------+----------+
              |          |          |
            ALLOW     CHALLENGE    BLOCK
```

**Core philosophy:**

> Identity tells you who something is.  
> Telemetry tells you what it did.  
> Trustvian determines whether what it did should be trusted.
