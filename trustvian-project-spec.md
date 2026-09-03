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

Potential future algorithms:

- N-gram behavioral models
- Markov models
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
package plus `event/` are public.

```text
trustvian/
├── trustvian.go, engine.go, options.go, result.go   # public API (root package)
├── event/                                              # public: Event, Actor, Operation, Target, Context
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
├── docs/
│   └── adr/
├── go.mod
├── LICENSE
├── README.md
└── Makefile
```

`internal/signal/{fft,complex,spectral}` (§8, the optional
signal-processing layer) and a dedicated `internal/sequence` package
remain future work — see [`docs/ROADMAP.md`](docs/ROADMAP.md).
`examples/`, a `Dockerfile`, and `CONTRIBUTING.md` are planned (Phase
5) but not present yet.

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

The OSS edition should provide real value:

- Core behavioral engine
- Feature extraction
- Fingerprinting
- Baseline engine
- Anomaly detection
- Trust scoring
- Policy engine
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

Potential enterprise features:

- Centralized management
- Multi-tenancy
- RBAC
- SSO / SAML / OIDC
- Policy management
- Behavioral profiles
- Historical analysis
- Security investigations
- Audit logs
- Service inventory
- Agent inventory
- Trust-score dashboards
- Alerting
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

Trustvian should understand:

- Agent identity
- Agent sessions
- Tool calls
- Tool sequences
- External calls
- File access
- Database access
- Secrets access
- Human approval
- Agent-to-agent communication
- Delegation
- Agent behavioral baselines

## 17. Policy Engine

Example:

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

## 18. Roadmap

The phases below are this document's original, long-term framing.
[`docs/ROADMAP.md`](docs/ROADMAP.md) is the authoritative, current
roadmap — organized by shippable milestone (`v0.1`/`v0.2`/`v0.3`/AI
Agent/Control), reconciled against what's actually implemented today,
with a detailed, independently-scoped task breakdown under
[`docs/tasks/`](docs/tasks/). Where the two differ (e.g. this
document's Phase ordering vs. `docs/ROADMAP.md`'s milestone grouping),
`docs/ROADMAP.md` reflects the real, current plan.

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

## 19. Business Model

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

## 20. Differentiation

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

## 21. Brand Architecture

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

## 22. Brand Messaging

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

## 23. Claude Code Implementation Instructions

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

## 24. Initial MVP Acceptance Criteria

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

## 25. Long-Term Vision

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
