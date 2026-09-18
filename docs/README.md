# Trustvian Developer Documentation

Practical documentation for building with and on Trustvian. For the
project's long-term vision and positioning, see
[`../trustvian-project-spec.md`](../trustvian-project-spec.md). For
engineering conventions this codebase follows, see
[`../CLAUDE.md`](../CLAUDE.md) and [`../.claude/rules/`](../.claude/rules/).

| Document | What's in it |
|---|---|
| [Getting Started](getting-started.md) | Install the CLI and SDK, run your first analysis |
| [Architecture](ARCHITECTURE.md) | System diagram, the pipeline, package layout, dependency direction, storage/OTel boundaries, relationship to Control/Cloud |
| [Domain Model](DOMAIN.md) | Event, Feature, Fingerprint, Baseline, Anomaly, Trust, Risk, Policy, Decision, and how they relate |
| [Go SDK Guide](sdk-guide.md) | `Event`, `Engine`, `Analyze`/`Observe`, `Result`, options, a worked baseline-maturity example |
| [CLI Guide](cli-guide.md) | `trustvian analyze` / `trustvian baseline build`, the event JSON format |
| [Policy Guide](policy-guide.md) | Writing `Rule`/`Condition` policies, fail-closed behavior, worked examples |
| [Anomaly Configuration Guide](anomaly-config-guide.md) | Public `AnomalyConfig` field reference, defaults/ranges, Go/YAML/CLI configuration |
| [Storage Guide](storage-guide.md) | The three Store backends (memory/file/PostgreSQL), when to use each, configuration, lifecycle, fail-closed behavior, concurrency, credentials, schema |
| [Operations](operations.md) | Backup, restore, upgrade, and rollback of learned state: what to back up, restore safety and verification, the compatibility matrix, and a recovery drill |
| [Reference Deployment](../deployments/docker-compose/README.md) | Runnable Docker Compose stack: OTel Collector + Trustvian + PostgreSQL, with a persistence proof and a smoke test |
| [Sequence Analysis](sequence-analysis.md) | Order-aware detection (`transition_deviation`), the bounded state model, cold start, activation |
| [OpenTelemetry Adapter](OPENTELEMETRY.md) | How `internal/otel` maps a span to an `Event`, the attribute mapping table, what's not yet implemented |
| [Observability](observability.md) | Operational metrics the Collector processor emits, their attributes and cardinality bound, what is deliberately not emitted, instrumentation cost, and the runtime's resource bounds |
| [Supply Chain](supply-chain.md) | The official container image: contents, tags, architectures, vulnerability policy, signing, and how to verify an image |
| [Branching Strategy](BRANCHING_STRATEGY.md) | Where work happens: `main`, short-lived branches, pull requests, immutable release-candidate and stable tags, hotfix and maintenance rules |
| [Release Guide](release-guide.md) | How releases are produced, what they contain, verifying downloaded artifacts, and recovering from a partial release |
| [Security Model](SECURITY.md) | Threats considered (spoofing, baseline poisoning, policy bypass, ...), implemented vs. future |
| [Performance](PERFORMANCE.md) | Hot paths, measured benchmark results, allocation/concurrency notes |
| [Roadmap](ROADMAP.md) | What's implemented, in progress, planned, and explicitly out of scope |
| [Use Cases](use-cases.md) | Four real scenarios (API anomaly, AI-agent security, service-to-service, valid-identity/abnormal-behavior) with verified input/output |
| [Architecture Decision Records](adr/) | Why: hexagonal core, public API boundary, OTel as a single-module adapter, narrow Store port, the fingerprint-dedup fix |

All code, command output, and benchmark numbers in these documents were
actually run against this repository, not hand-written or estimated —
see each file for how to reproduce them yourself.
