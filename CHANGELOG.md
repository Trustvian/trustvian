# Changelog

All notable changes to Trustvian are documented in this file. Prior to
`v0.1`, the project was pre-release iteration on a single evolving
`develop` branch with no tagged snapshots — this file starts at the
point where a tag first exists for something external users can
actually depend on.

## v0.1.0 — Behavioral core hardening & first public release

The core pipeline — `Event → Features → Fingerprint → Baseline →
Anomaly → Trust → Policy → Decision` — was already implemented, tested,
and benchmarked before this milestone. `v0.1` is the pass that took it
from "works and is tested" to "hardened, explainable, benchmarked,
demonstrated, and released as a stable OSS artifact." No new pipeline
stages were added; this release is depth, not breadth.

### Added

- **Target category** ([task 001](docs/tasks/001-feature-model.md)) —
  `event.Event.Target.Category` (`internal`, `external`, `database`) is
  a new, optional stable dimension. It flows through
  `internal/features.Extract` into `StableFeatures` and, from there,
  into the behavioral `Fingerprint`, so an actor that has only ever
  called internal services suddenly calling an external one is now a
  detectable category shift, not just a specific-hostname novelty.
  Zero value (`unspecified`) is accepted by `Event.Validate()` and
  never required.
- **Fingerprint versioning** ([task 002](docs/tasks/002-fingerprint.md))
  — `internal/fingerprint.Compute` now writes an explicit version
  marker into its hash before the stable fields. A future change to
  which dimensions feed the hash (or to the hash algorithm) bumps this
  one constant, producing a disjoint ID space instead of silently
  reinterpreting old `Fingerprint.ID`s under new semantics — the thing
  that stops being harmless once a persistent `Store` is in play (see
  `store.FileStore`, shipped in task 003). The fingerprint's design —
  what feeds the hash, in what order, and why FNV-1a — is now written
  up in [DOMAIN.md § Fingerprint](docs/DOMAIN.md#fingerprint).
- **`frequency_deviation` anomaly signal**
  ([task 004](docs/tasks/004-anomaly.md)) — `internal/baseline` now
  tracks an EWMA of inter-observation intervals
  (`FingerprintStats.IntervalMean`/`IntervalVariance`/
  `IntervalObservations`), and `internal/anomaly.Score` uses it to
  detect abnormal request *frequency*: an actor that normally calls an
  operation every 10 seconds and suddenly calls it every 100ms now
  triggers a dedicated signal, not just categorical novelty. Zero-cost
  (no allocation) on the common "frequency is normal" path, gated by
  `Config.FrequencyZThreshold`/`FrequencyWeight`, matching the existing
  latency/error signal shape exactly.
- **`Trust.Explain()`** ([task 005](docs/tasks/005-trust-risk.md)) — a
  new method rendering a `Trust` value as a short, human-readable
  sentence (identity confidence, anomaly at its effective confidence,
  context risk, and the resulting risk level). The multiplicative trust
  formula itself is unchanged; a new scenario-matrix test now sweeps
  identity confidence, anomaly score/confidence, and context risk
  across representative ranges and asserts the formula never produces a
  value outside `[0,1]` and is monotonic in every input.
- **Attribute matching in policy `Condition`**
  ([task 006](docs/tasks/006-policy.md)) — `policy.Condition` gained an
  `Attributes map[string]string` field, ANDed with every other
  `Condition` field, closing the original spec's own
  `tool.category: secrets` policy example. This is flat key/value
  equality only — no AND/OR/NOT combinators, no comparison operators,
  and no dynamic policy loading were added; `Condition{}`'s zero-value
  "matches everything" behavior is unchanged.
- **`Result.Explain()`** ([task 007](docs/tasks/007-decision.md)) — a
  new method rendering a complete, human-readable decision summary:
  final decision, trust/risk/anomaly scores, every contributing
  anomaly signal with its detail, and the matched policy rule (or
  default reason). Every field the project's own Decision checklist
  names was already present on `Result`; this makes assembling them
  into a readable explanation a reusable SDK method instead of
  CLI-only formatting logic.
- **`examples/`** ([task 010](docs/tasks/010-examples.md)) — a new,
  runnable `examples/` directory with six self-contained programs
  (`basic`, `credential-misuse`, `unexpected-dependency`,
  `external-destination`, `frequency-abuse`, `ai-agent`), each a
  genuine external consumer of this module (via `go mod replace`),
  each demonstrating the full `Event → ... → Decision` path with real,
  captured `go run` output. `make examples` runs and verifies all six.
- **Two closed performance-measurement gaps**
  ([task 011](docs/tasks/011-performance.md)) —
  `BenchmarkEventFromSpan` (`internal/otel`) and
  `BenchmarkInMemoryMemoryGrowth` (`internal/store`, at 100/1,000/10,000
  distinct keys) close the two gaps [PERFORMANCE.md](docs/PERFORMANCE.md)
  had explicitly named as unmeasured. Every pipeline stage named in the
  project roadmap is now benchmarked, with numbers reproduced fresh as
  part of this release (see [PERFORMANCE.md § Measured
  results](docs/PERFORMANCE.md#measured-results)).
- **Dedicated security test suite**
  ([task 012](docs/tasks/012-security-tests.md)) — new tests for
  malformed/extreme input (`NaN`/`±Inf` `IdentityConfidence`, very long
  strings, negative `duration_ms`), resource-exhaustion safety (a
  100,000-key `Attributes` map, a single actor producing 5,000 distinct
  fingerprints — neither panics), and an explicit end-to-end proof of
  cross-actor `Baseline` isolation
  (`TestAnalyzeCrossActorIsolation`). [SECURITY.md](docs/SECURITY.md)
  now documents every threat named in the project roadmap, each with a
  test reference or an explicit deferred-mitigation label.

### Changed

- [docs/ROADMAP.md](docs/ROADMAP.md)'s "Current status" section now
  reflects `v0.1` as shipped; `v0.2` (OpenTelemetry maturation) is the
  new "next up."

### Verified, not changed

`v0.1` is a hardening release: the pipeline shape, the trust formula,
and the policy evaluator's fail-closed guarantee are all unchanged from
before this milestone. What changed is depth — a new stable dimension,
a new anomaly signal, a versioned fingerprint hash, richer policy
matching, and, above all, verification: broader test coverage, a
documented performance baseline, and a documented security posture.

## Public API compatibility promise

Starting at `v0.1.0`, the following are committed stable — a breaking
change to any of them will be called out explicitly in a future
changelog entry and reflected in a major/minor version bump, not made
silently:

- **`event.Event`'s public field shapes** — `Actor`, `Operation`,
  `Target`, `Context`, `Attributes`, and the enum types backing them
  (`ActorType`, `OperationCategory`, `OperationDirection`,
  `TargetCategory`). New optional fields may be added in a
  backward-compatible way (zero value = "unset", never required by
  `Validate()`); existing fields will not be removed, renamed, or have
  their meaning changed.
- **`Result`'s public field shapes** — `Event`, `Features`,
  `Fingerprint`, `BaselineKey`, `Anomaly`, `Trust`, `Decision`,
  `Explanation`, and `Result.Explain()`'s general contract (a
  human-readable summary covering decision, scores, contributors, and
  policy reason — exact wording is not part of the promise, field
  presence is).
- **`Engine`'s public method signatures** — `NewEngine(opts ...Option)`,
  `Analyze(ctx context.Context, ev event.Event) (Result, error)`, and
  `Observe(ctx context.Context, result Result) (learned bool, err error)`.
- **The `Option` functions** — `WithStore`, `WithPolicy`,
  `WithAnomalyConfig`, `WithTrustConfig`, `WithContextRisk`, and the
  `Option func(*Engine)` type itself.

**Everything under `internal/` carries no compatibility promise.**
This includes, but is not limited to, `internal/features.Features`,
`internal/fingerprint.Fingerprint`, `internal/anomaly.Anomaly` and
`anomaly.Config`, `internal/trust.Trust` and `trust.Config`,
`internal/policy.Policy`/`Condition`/`Rule`, and
`internal/store.Store` and its implementations. External callers can
already *read* these types' exported fields through a `Result` value
without importing them (see [ADR
0002](docs/adr/0002-public-api-boundary.md)), but cannot construct or
name them directly from outside this module — Go's `internal/`
visibility rule enforces that boundary, and this project reserves the
right to change anything inside it, including field shapes, without
that counting as a breaking change to the public API described above.
