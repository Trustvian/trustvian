# 0017 — Public anomaly configuration boundary

## Context

[Task 032](../tasks/032-agent-security-scenario-validation.md) found,
while building a fully-public example, that `trustvian.WithAnomalyConfig`
takes an `internal/anomaly.Config` value directly. An external module
cannot spell that type — Go's `internal/` import restriction blocks it
outright, regardless of any `replace` directive — and, unlike
`policy.Policy`, no public function existed anywhere that returned one
for pass-through. Concretely: an OSS consumer had no way, by any
means, to enable `DelegationWeight`, `NGramWeight`, `TransitionWeight`,
or any other `v0.6`/`v0.7` signal weight, ever, from outside this
module. [Task 033](../tasks/033-v07-stabilization-release-gate.md)
closes that gap, the same way `v0.5` closed the identical gap for
`policy.Policy` (ADR 0008).

## Decision: `config.AnomalyConfig`, a third independent document

`config` gains a third public, declarative document —
`AnomalyConfig` — compiled and validated by `CompileAnomaly` into the
`anomaly.Config` value `trustvian.WithAnomalyConfig` accepts. It is a
separate document from `PolicyConfig` and `AlertConfig`, not a section
nested inside either, for the identical reason `AlertConfig` is
already separate from `PolicyConfig` (ADR 0009): three genuinely
different questions — "what decision should Trustvian make," "which
Results should notify," "how should Trustvian score behavioral
deviation" — stay independently compiled and independently versioned
(`AnomalySchemaVersionV1`, decoupled from `SchemaVersionV1`/
`AlertSchemaVersionV1`, per ADR 0008's own schema-versioning
reasoning), rather than merging into one combined schema before a real
consumer needs one.

```
config.AnomalyConfig  →  CompileAnomaly  →  anomaly.Config  →  Engine
```

## Every existing field is exposed — there is no internal-only remainder

Before writing `AnomalyConfig`, every field of `internal/anomaly.Config`
was individually classified (public configuration / internal
implementation detail / derived value / deprecated / experimental).
The result: all fifteen fields are legitimate, already-documented,
operator-facing configuration — `MinObservations`,
`LatencyZThreshold`/`FrequencyZThreshold`,
`NoveltyWeight`/`LatencyWeight`/`ErrorWeight`, `FrequencyWeight`,
`TimePatternWeight`, `TransitionWeight`/`MinTransitionObservations`/
`TransitionRarityWeight`, `NGramWeight`/`MinNGramObservations`/
`NGramRarityWeight`, `MarkovWeight`, `DelegationWeight`, and
`SensitiveTargetFloor`. None is a cache size, an internal buffer, or a
derived value with no operator-facing meaning — every one already
ships with its own deliberate default and its own calibration story in
`internal/anomaly/anomaly.go`'s existing doc comments. `AnomalyConfig`
mirrors this set exactly; it invents nothing.

**What stays internal, and why.** `internal/baseline`'s resource-bound
constants — `maxPredecessors`, `maxTrigramPredecessors`,
`maxDelegators` (all 64) — are deliberately **not** part of
`AnomalyConfig`, and not part of `anomaly.Config` either; they are not
anomaly-scoring configuration at all, but fixed state-size bounds a
config file has no business raising. Making them operator-configurable
would open exactly the `MaxEntries = unlimited`-style misuse this
task's own brief warned against, for no proven operator need — every
one of them already has a documented, deliberately generous default
(see `internal/baseline/baseline.go`'s own doc comments on each), and
raising them is a distinct, future decision, not something to expose
speculatively alongside an unrelated config boundary fix.

## Zero-value semantics: two rules, not one, chosen deliberately

Every `AnomalyConfig` field falls into exactly one of two categories,
matching the identical distinction `AlertConditionConfig.MinAnomalyScore`
(plain value) vs. `MaxTrustScore` (pointer) already established:

- **Canonical default is already the Go zero value** (every `*Weight`
  field except `NoveltyWeight`/`LatencyWeight`/`ErrorWeight`): a plain,
  non-pointer `float64`. Omitting the field in YAML, or leaving it at
  its zero value in a Go literal, means exactly what
  `anomaly.DefaultConfig()` already means by "unset": opt-in,
  disabled. No ambiguity to resolve — 0 already is "unspecified."
- **Canonical default is genuinely non-zero**
  (`MinObservations`=20, `LatencyZThreshold`/`FrequencyZThreshold`=3.0,
  `NoveltyWeight`=1.0, `LatencyWeight`=0.6, `ErrorWeight`=0.8,
  `MinTransitionObservations`/`MinNGramObservations`=20): a pointer.
  `nil` means "use `anomaly.DefaultConfig()`'s own value"; a non-nil
  pointer means "use this value instead." A plain `float64`/`uint64`
  here would make "the caller didn't set this" indistinguishable from
  "the caller explicitly chose the Go zero value" — for
  `NoveltyWeight` specifically, that would silently disable
  `categorical_novelty` for any caller who simply didn't set every
  field, a real "never silently weaken a security-relevant default"
  violation (CLAUDE.md), not a cosmetic inconsistency.

This is why `CompileAnomaly(AnomalyConfig{Version: "v1"})` — every
pointer nil, every plain weight at its Go zero value — reproduces
`anomaly.DefaultConfig()` byte-for-byte, proven by
`TestCompileAnomalyZeroValueMatchesDefaultConfig`, not merely intended:
existing `v0.1`–`v0.7` callers who configure nothing continue to see
byte-for-byte unchanged behavior after this task, the same guarantee
`CompilePolicy(PolicyConfig{})`... actually never held (Policy's
default requires `DefaultDecision`/`DefaultReason`, deliberately, per
its own fail-closed design) — `AnomalyConfig` does not need that
requirement, because there is no failure mode here analogous to an
unconfigured `Policy` silently allowing everything: an unconfigured
`AnomalyConfig` simply reproduces the existing, already-safe
`anomaly.DefaultConfig()`.

## Validation

`AnomalyConfig.Validate()` follows the identical "first error wins"
convention `PolicyConfig.Validate`/`AlertConfig.Validate` already use.
Every weight-shaped field (plain or pointer) is validated as finite
and within `[0, 1]`, reusing the existing `ErrInvalidThreshold`
sentinel and `validThreshold` helper `AlertConditionConfig` already
established — weights and thresholds share the identical documented
range (`Anomaly.Score`'s own bound), so introducing a second sentinel
for the same check would be pure duplication. Z-score threshold fields
use a new, distinct check (`validZThreshold`/`ErrInvalidZThreshold`:
finite and `> 0`) — a z-score threshold is a standard-deviation
multiple, not a `[0, 1]`-bounded probability-like value, so reusing
`validThreshold` there would silently accept `0` (division-by-zero
territory downstream) and reject legitimate values above `1`.
`MinObservations`/`MinTransitionObservations`/`MinNGramObservations`
have no invalid `uint64` value: `0` is a genuine, internally-handled
configuration (`anomaly.Score`'s own familiarity computation treats it
as "everything is immediately mature"; `transitionRaritySignal`/
`markovSurprisalSignal` treat a `0` minimum as "any nonzero total is
enough") — not an error to guard against, at any layer.

## Compile-once, not per-event

`CompileAnomaly` is called exactly once, at `Engine` construction time
— identical to `CompilePolicy`'s own existing discipline. Nothing in
`Engine.Analyze`'s hot path re-validates or re-converts
`AnomalyConfig`; `Engine` stores only the already-compiled
`anomaly.Config` value. `BenchmarkCompileAnomaly` measures this
one-time cost in isolation (tens of nanoseconds, one allocation);
`BenchmarkEngineAnalyzeDelegationAbsent`'s own unchanged allocation
profile (`456 B/op, 17 allocs/op`, identical to `BenchmarkEngineAnalyze`)
confirms the hot path itself carries none of this cost.

## Surface parity

`config.LoadAnomaly`/`LoadAnomalyFile` give the Go SDK and the CLI
(`trustvian analyze/baseline build --anomaly-config <path>`) a shared,
canonical loading path — the CLI does not re-implement parsing or
validation, mirroring exactly how it already delegates
`--config`/`PolicyConfig` to the same `config` package. The OTel
Collector processor does not yet gain parity: `processor/go.mod` still
pins `github.com/Trustvian/trustvian v0.5.0` (predating even task
014's `ApprovalStatus`/`DelegatedFrom` fields), so this new public
surface — like every other `v0.6`/`v0.7` capability — is not yet
reachable from the processor at all. This is not a gap this task
introduces or must close; it resolves automatically once a future task
bumps `processor/go.mod` to depend on a tagged `v0.7.0`.

## Consequences

- One new public document (`AnomalyConfig`), one new compiler
  (`CompileAnomaly`), two new loaders (`LoadAnomaly`/`LoadAnomalyFile`),
  one new CLI flag (`--anomaly-config`). No new package, no new
  dependency, no change to `internal/anomaly` itself.
- Existing callers who configure nothing continue to see
  `anomaly.DefaultConfig()`'s own behavior, byte-for-byte, proven by
  test.
- `internal/baseline`'s resource-exhaustion bounds remain internal,
  deliberately — a config surface for them, if ever justified, is
  separate future work.
- The OTel Collector processor gains no new capability yet — it
  remains pinned to `v0.5.0` until a future task bumps it.
