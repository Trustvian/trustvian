# 023 — Declarative Alert Configuration

**Milestone:** v0.5 · **Depends on:** [018](018-alert-notification-foundation.md)
(the public `alert` package this task consumes, unmodified) ·
**Independent of:** [019](019-policy-config-model.md)/[020](020-policy-config-loader.md)/[021](021-cli-config-integration.md)/[022](022-collector-config-integration.md)
(this task reuses their package, YAML-decoding, and validation
*conventions*, but shares no code path or document shape with
`PolicyConfig` — see [ADR 0009](adr/0009-alert-config-is-a-separate-document.md))

## Objective

Let a caller declare Alert Evaluation rules (`[]alert.Rule`) in YAML,
the same way [task 019](019-policy-config-model.md) let a caller
declare a `Policy` — without merging Policy and Alert into one
generic rule language, and without touching `PolicyConfig`'s existing,
released schema v1 at all.

## Why

`docs/ROADMAP.md`'s own "not yet true" list has said, since `v0.4.0`,
that declarative Alert configuration does not exist:
`alert.Rule` values can only be constructed by Go code living inside
whatever process calls `alert.Evaluate`. This is the exact gap task
019 closed for `Policy`, applied to the structurally independent
`alert` package.

## Scope

```text
AlertConfig
      ↓  Validate
      ↓  CompileAlerts
[]alert.Rule
      ↓
alert.Evaluate(Result, []alert.Rule) (Alert, bool)   — unchanged
```

- New public types in `config` (`config/alert.go`):
  `AlertSchemaVersionV1`, `AlertConfig{Version, Rules}`,
  `AlertRuleConfig{Name, When, Severity}`,
  `AlertConditionConfig{Decision, MinRiskLevel, ActorType,
  TargetCategory, MinAnomalyScore, MaxTrustScore}` — primitive-typed
  structs mirroring `alert.Rule`/`alert.Condition` exactly, the same
  discipline `PolicyConfig`/`PolicyRule`/`PolicyCondition` already
  established for `policy.Policy`/`Rule`/`Condition`.
- `(AlertConfig).Validate() error` (`config/validate.go`): recognized
  schema version; each rule has a unique, non-empty, bounded-length
  name and a valid `Severity`; each condition's `Decision`/
  `MinRiskLevel`/`ActorType`/`TargetCategory` are valid enum values;
  `MinAnomalyScore`/`MaxTrustScore` are finite and within `[0, 1]`
  (rejecting NaN, ±Inf, negative, and >1 values) — the documented
  range both `Anomaly.Score` and `Trust.Score` are always bounded to.
- `CompileAlerts(AlertConfig) ([]alert.Rule, error)`
  (`config/compile.go`): validates, then translates field-for-field —
  no matching logic of its own, no coupling to `Policy`/
  `CompilePolicy` at all. Preserves `Rules`' slice order exactly
  (first-match-wins, matching `alert.Evaluate`'s own contract).
- `LoadAlerts([]byte) (AlertConfig, error)` /
  `LoadAlertsFile(path string) (AlertConfig, error)`
  (`config/load.go`): the same strict-decoding discipline
  `Load`/`LoadFile` already established (`go.yaml.in/yaml/v3`,
  `KnownFields(true)`, duplicate-key rejection,
  `maxConfigFileSize`-bounded reads) — no second, parallel YAML
  parser, no new dependency.

## Non-Goals

- **`PolicyConfig`/`Load`/`LoadFile`/schema v1 are not modified in any
  way.** See [ADR 0009](adr/0009-alert-config-is-a-separate-document.md)
  for why a combined schema-v2 document was considered and rejected for
  this task.
- **No merged `PolicyRule`/`AlertRuleConfig` type.** Non-negotiable —
  see ADR 0009's "Alternatives considered."
- **No CLI integration** (`--alert-config`, or reusing `--config` for
  alerts). The CLI has no alert-delivery flow today for a compiled
  `[]alert.Rule` to plug into — `trustvian analyze` prints a Policy
  decision report, it does not send webhooks, and building one now
  would be exactly the "turn `analyze` into a webhook delivery daemon"
  scope creep this task's own brief warns against. This is a deferred
  decision, not a gap: a future task can add it once there is a real
  CLI flow (e.g. a `--dry-run-alerts` report mode) to justify it.
- **No Collector integration** (`alerts:` in Collector config). Same
  reasoning: `processor/` has no alert-delivery path today, and this
  task does not add one.
- **No Alert delivery/reliability configuration** — no `sinks:`,
  `webhook:`, retry, deduplication, or cooldown config. The Alert
  Reliability stage (`docs/ROADMAP.md` § Alert & Notification phase)
  remains entirely separate and unscoped.
- **No new `alert.Condition`/`alert.Rule`/`alert.Evaluate` semantics.**
  This task translates existing semantics into config; it does not
  extend them. No architectural incompatibility was discovered that
  would require otherwise.
- **No numeric expression language** for `when:` blocks beyond what
  `alert.Condition` already supports natively (flat,
  AND-of-optional-fields) — matching `PolicyCondition`'s own
  established non-goal.

## Technical Requirements

- Every pre-existing `config` test (all `PolicyConfig`/`Load`/
  `LoadFile`/`CompilePolicy` tests) passes completely unmodified —
  the structural proof that this task did not touch schema v1's
  behavior at all.
- `AlertConfig.Validate()` fails closed the same way
  `PolicyConfig.Validate()` does: the first invalid field anywhere
  (schema version, rule name, severity, condition enum value, or
  threshold) is returned as a specific, actionable `errors.Is`-checkable
  sentinel error — never silently ignored, never a generic message.
- Unlike `PolicyConfig`, `AlertConfig` has no `DefaultDecision`/
  `DefaultReason` to require, and an empty `Rules` list is valid —
  this deliberately mirrors `alert.Evaluate`'s own documented
  asymmetry with `policy.Policy.Evaluate` (no match means no alert,
  not a fail-closed error), not an oversight or an inconsistency with
  `PolicyConfig`.
- `CompileAlerts` is pure (no I/O, no global state, no wall-clock
  dependency, no mutation of its input) and deterministic — proven by
  `TestCompileAlertsIsDeterministic`/`TestCompileAlertsDoesNotMutateConfig`,
  the same properties `TestCompilePolicyIsDeterministic`/
  `TestCompilePolicyDoesNotMutateConfig` already prove for
  `CompilePolicy`.
- Rule order from a YAML `rules:` sequence survives decoding and
  compilation unchanged — proven by
  `TestCompileAlertsPreservesRuleOrder` and `TestLoadAlertsValidDocument`
  (which asserts on decoded rule order directly).

## Tests

`config/alert_test.go` — `AlertConfig.Validate()`: minimal/full valid
configs, unsupported version, empty-rules acceptance, empty/duplicate
rule name, invalid severity/decision/risk level/actor type/target
category, invalid `MinAnomalyScore`/`MaxTrustScore` (negative, >1, NaN,
±Inf), too many rules.

`config/alert_compile_test.go` — `CompileAlerts`: invalid-config
rejection, rule-order preservation, determinism, non-mutation,
every-field translation, and
`TestEndToEndConfiguredAlertProducesExpectedAlert` — this task's own
central acceptance test: a real `AlertConfig`, compiled via
`CompileAlerts`, evaluated via the real, unmodified `alert.Evaluate`
against a real `Result` from a real `Engine.Analyze` call, produces
the expected `Alert` (matching severity, fingerprint) for a matching
`Result`, and no `Alert` at all for a non-matching one — proving the
whole `Config → CompileAlerts → []alert.Rule → alert.Evaluate → Alert`
path, not just that decoding/compilation succeeds.

`config/alert_load_test.go` — `LoadAlerts`/`LoadAlertsFile`: valid
document (with rule-order assertion), empty input, unknown field,
duplicate YAML key, invalid config, missing file, oversized file, and
`FuzzLoadAlerts` (the same "arbitrary bytes must never panic" invariant
`FuzzLoad` already proves for `PolicyConfig`).

`config/alert_bench_test.go` — `BenchmarkCompileAlerts`/
`BenchmarkValidateAlerts`, mirroring `BenchmarkCompilePolicy`/
`BenchmarkValidate`: startup-path cost, recorded for the record, not
optimized against (this is not a hot path — `CompileAlerts` runs once
per `[]alert.Rule` construction, never per `Result`).

## Documentation

- [docs/DOMAIN.md](DOMAIN.md) § Alert: new "Configuring Alerts from
  outside this module" paragraph.
- [docs/adr/0009-alert-config-is-a-separate-document.md](adr/0009-alert-config-is-a-separate-document.md)
  (new): the schema-v2-vs-separate-document decision.
- [README.md](../README.md): new "Configuring Alerts" section
  alongside the existing "Configuring a Policy" one.
- [docs/ROADMAP.md](ROADMAP.md): this task marked done under `v0.5`;
  `v0.5`'s "Alert configuration does not exist" gap closed.
- [docs/SECURITY.md](SECURITY.md): new Alert-config validation entry
  in the configuration-input-validation threat table.
- [CHANGELOG.md](../CHANGELOG.md): per this repository's own
  convention (one heading per tag, no `Unreleased` section — see task
  021's precedent), this task's work is folded into the `v0.5.0`
  release notes prepared for the still-pending human tag, not given
  its own interim entry.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new
  `config/alert_*_test.go` test, and every pre-existing `config` test
  unmodified and still passing.
- `go test -bench=Alert -benchmem ./config/...` runs and reports
  numbers (not optimized against — startup-path cost, recorded for the
  record).
- A real `AlertConfig`, compiled and evaluated against a real `Result`,
  produces the exact `Alert` `alert.Evaluate` is documented to produce
  — demonstrated end-to-end, not just via decoding/compilation tests.
- `gofmt -l .`, `go vet ./...` clean.
- No new dependency (`git diff go.mod go.sum` empty).
- `PolicyConfig`/`Load`/`LoadFile` are byte-for-byte unmodified (no
  diff in their own test expectations; every pre-existing test passes
  unchanged).
- No merged `PolicyRule`/`AlertRuleConfig` type exists anywhere.
- `config` does not import `internal/policy`'s or `internal/trust`'s
  types into `AlertConfig`/`AlertRuleConfig`/`AlertConditionConfig`
  themselves (they stay plain strings/floats, validated internally,
  exactly like `PolicyCondition`).
