# Trustvian Developer Documentation

Practical documentation for building with and on Trustvian. For the
project's long-term vision and positioning, see
[`../trustvian-project-spec.md`](../trustvian-project-spec.md). For
engineering conventions this codebase follows, see
[`../CLAUDE.md`](../CLAUDE.md) and [`../.claude/rules/`](../.claude/rules/).

| Document | What's in it |
|---|---|
| [Getting Started](getting-started.md) | Install the CLI and SDK, run your first analysis |
| [Architecture](architecture.md) | The pipeline, package layout, and why the boundaries are where they are |
| [Go SDK Guide](sdk-guide.md) | `Event`, `Engine`, `Analyze`/`Observe`, `Result`, options, a worked baseline-maturity example |
| [CLI Guide](cli-guide.md) | `trustvian analyze` / `trustvian baseline build`, the event JSON format |
| [Policy Guide](policy-guide.md) | Writing `Rule`/`Condition` policies, fail-closed behavior, worked examples |
| [OpenTelemetry Adapter](opentelemetry.md) | How `internal/otel` maps a span to an `Event`, the attribute mapping table |
| [Use Cases](use-cases.md) | Four real scenarios (API anomaly, AI-agent security, service-to-service, valid-identity/abnormal-behavior) with verified input/output |

All code and command output in these documents was actually run against
this repository, not hand-written — see each file for how to reproduce
it yourself.
