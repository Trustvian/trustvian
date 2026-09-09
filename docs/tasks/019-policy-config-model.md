# 019 — Public Policy Configuration Model + Compiler

**Milestone:** v0.5 · **Depends on:** v0.1 shipped (stable
`internal/policy.Policy`/`Condition`/`Rule`/`Decision` shapes to
compile against) · **Blocks:** file parsing/schema-v1 loading, CLI
integration, and OTel Collector processor integration (all separate,
later v0.5 tasks — see Non-Goals)

## Objective

Give an external caller — Go SDK user, CLI, OTel Collector processor,
or any standalone deployment — a way to construct a meaningful,
custom `Policy` without writing code inside this module or importing
`internal/policy`, closing the gap [ADR 0002](../adr/0002-public-api-boundary.md)
named. This is the first, narrowest vertical slice of
[ROADMAP.md § v0.5](../ROADMAP.md#v05--policy--configuration): the
public configuration *model* and its *compiler* into a real
`policy.Policy`, entirely in Go — no file parsing, no CLI flag, no
Collector integration yet (each is its own later task).

## Motivation

Today, `trustvian.WithPolicy` takes a `policy.Policy` — a real,
working, evaluable type — but nothing outside this module can
construct one, since `Policy`/`Condition`/`Rule`/`Decision` all live
under `internal/policy`. This is not a theoretical gap:
`processor/` (a genuinely separate module) resolves every span to
`observe_only` today for exactly this reason (see
[`processor/README.md` § Configuration](../../processor/README.md#configuration)).
A declarative config *file* would still hit this same wall without a
public model and compiler underneath it — this task builds that
foundation first.

## Scope

- A new public package, `config`
  (`github.com/Trustvian/trustvian/config`), sibling to `event` and
  `alert` — required because `processor/` and any standalone
  deployment are genuinely separate Go modules, which cannot import an
  `internal/` package (see [ADR 0008](../adr/0008-policy-config-boundary.md)
  for the full placement reasoning).
- `PolicyConfig`, `PolicyRule`, `PolicyCondition` — primitive-typed
  public structs describing exactly what `policy.Policy`/`Rule`/
  `Condition` already support today: `ActorType`, `OperationCategory`,
  `TargetName`, `Environment`, `MinRiskLevel`, `Attributes` as
  matchable dimensions; `Decision`/`Reason` as outcomes;
  first-match-wins `Rules` order; a mandatory, validated
  `DefaultDecision`/`DefaultReason`. No matchable dimension is invented
  that `policy.Condition` cannot already evaluate (in particular, no
  `anomaly_score`/`trust_score` numeric matcher — `policy.Condition`
  has no such field today; adding one is a separate, future decision
  about `internal/policy` itself, not this task's to make).
- `(PolicyConfig).Validate() error` — fails closed on: unsupported/
  missing schema version, missing or invalid default decision/reason,
  invalid decision on any rule, invalid `actor_type`/
  `operation_category`/`min_risk_level`, empty or duplicate rule names,
  empty reasons, overlong names, too many rules. Returns the first
  problem found (matching `event.Event.Validate()`'s existing
  "first error wins" convention), with an actionable, field-path-
  identifying message.
- `CompilePolicy(cfg PolicyConfig) (policy.Policy, error)` — validates
  (always, even if the caller already called `Validate()`) and
  translates `cfg` into a real `policy.Policy`, preserving rule order
  exactly, doing no evaluation of its own. The returned `policy.Policy`
  is usable by a caller outside this module via type inference,
  without ever importing `internal/policy` — see
  [ADR 0008](../adr/0008-policy-config-boundary.md) for the empirical
  verification behind this claim.
- `SchemaVersionV1` — the one supported config schema version today,
  explicitly independent of the Trustvian release version.

## Non-Goals

- **No file parsing (YAML/JSON) of any kind.** `PolicyConfig` values
  are constructed directly in Go in this task; a loader (task
  scoped separately, once this model is stable — see
  [ROADMAP.md § v0.5](../ROADMAP.md#v05--policy--configuration)) reads
  a file and produces the same `PolicyConfig` struct this task defines.
- **No CLI integration.** `cmd/trustvian` is not touched.
- **No OTel Collector processor integration.** `processor/` is not
  touched — it continues running the default `Policy` until its own,
  later task wires this package in, respecting the module boundary
  (`processor/` importing `github.com/Trustvian/trustvian/config`, a
  normal external dependency, never a duplicate config struct of its
  own).
- **No Alert configuration.** `alert.Rule`/`Condition` are not
  touched; `PolicyConfig` and any future `AlertConfig` remain separate
  Go types compiled independently, even if a later task's file format
  nests both under one document — see
  [ROADMAP.md § v0.5](../ROADMAP.md#v05--policy--configuration)'s own
  explicit note that `Policy → Decision` and `Decision/Result → Alert
  Evaluation` stay structurally independent.
- **No general expression language, no boolean combinators beyond
  `When`/`Unless`.** `PolicyCondition` is a direct, flat mirror of
  `policy.Condition` — AND-of-optional-fields, nothing more.
- **No live config reload.** `CompilePolicy` produces one immutable
  `policy.Policy`; hot-swapping it in a running `Engine` is a distinct
  future concern (atomic replacement, partial-failure semantics,
  rollback) explicitly out of scope here.
- **No promotion of `internal/policy` types to a public package.**
  `Policy`/`Condition`/`Rule`/`Decision` all remain `internal/` — see
  [ADR 0008](../adr/0008-policy-config-boundary.md) for why this is
  sufficient and deliberate, not a gap deferred to a later task.

## Technical Requirements

- `config` package: no I/O, no global/package-level mutable state
  (per `.claude/rules/go.md`), no dependency beyond the standard
  library plus this module's own `event`, `internal/policy`,
  `internal/trust` packages.
- `CompilePolicy` is pure and deterministic: the same `PolicyConfig`
  value always compiles to a `reflect.DeepEqual`-identical
  `policy.Policy`, proven by test, not merely argued — rule order is
  preserved via slice iteration only, never a map.
- `Validate`/`CompilePolicy` never mutate their `PolicyConfig`
  argument — proven by test.
- Validation is defense-in-depth against config-time typos that would
  otherwise become *silent* policy weakenings at evaluation time (e.g.
  a typo'd `actor_type` compiles to a `Condition` that simply never
  matches — silently neutering a rule an operator believes is active).
  This is why validation is stricter here than
  `policy.Policy.Evaluate`'s own runtime fail-closed behavior: a
  config-time error is far more actionable than a same-shaped runtime
  failure with no reference back to the config file that caused it.
- Resource bounds (`maxRules = 1000`, `maxNameLength = 256`) are
  documented with their rationale in code, not asserted without
  justification — config is human-authored operational input, not
  per-event telemetry volume, so these are deliberately far stricter
  than this codebase's existing runtime bounds (e.g. the 100,000-key
  `Attributes` map `internal/security`'s tests already probe without
  panicking).

## Tests

- `config/validate_test.go`: minimal valid config; full valid config
  (multiple rules, `When`/`Unless`, `Attributes`); missing/unsupported
  version; missing default decision/reason (each independently);
  invalid default decision; invalid rule decision; empty rule name;
  missing reason; duplicate rule name; invalid `actor_type`/
  `operation_category`/`min_risk_level` (including inside `Unless`);
  a zero-value `PolicyCondition` is explicitly *accepted* (legitimate
  catch-all semantics, not a mistake); too many rules; overlong name.
- `config/compile_test.go`: `CompilePolicy` rejects an invalid config
  (delegates to `Validate`); preserves rule order; is deterministic
  (compile twice, `reflect.DeepEqual`); does not mutate its input;
  every `PolicyCondition` field translates correctly. The load-bearing
  test: `TestEndToEndConfiguredPolicyProducesConfiguredDecision` —
  `PolicyConfig` → `CompilePolicy` → `trustvian.WithPolicy` → a real
  `Engine.Analyze` call → a `Decision` the *configured* rule produced,
  and a second event proving the configured default applies when no
  rule matches. `TestCompiledPolicyIsSafeForConcurrentAnalyze`: many
  goroutines calling `Analyze` against one compiled `Policy`,
  race-clean.

## Benchmarks

`BenchmarkCompilePolicy`/`BenchmarkValidate` on a moderately-sized,
realistic config — startup-path work (compiled once per `Engine`
construction, not per `Analyze` call), so measured for the record
rather than optimized against, per this codebase's own "don't obsess
over nanoseconds on a cold path" precedent.

## Documentation

- [DOMAIN.md § Policy and Decision](../DOMAIN.md#policy-and-decision):
  document the `config` package's relationship to `internal/policy`.
- [ARCHITECTURE.md](../ARCHITECTURE.md): note `config` as a third
  public package (alongside `event`/`alert`), and the pass-through
  pattern that keeps `internal/policy` internal.
- [SECURITY.md](../SECURITY.md): a new entry for configuration-input
  validation as a security boundary.
- [policy-guide.md](../policy-guide.md): a new section introducing
  `config.PolicyConfig` as the declarative-adjacent, Go-constructible
  alternative to a hand-built `policy.Policy` literal.
- [ROADMAP.md](../ROADMAP.md): mark this task done under `v0.5`; keep
  file parsing/CLI/processor integration explicitly not-yet-started.
- New [ADR 0008](../adr/0008-policy-config-boundary.md): why `config`
  is public while `internal/policy` stays internal, the schema
  versioning strategy, and the consumer model.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new `config`
  test.
- `CompilePolicy`'s output is proven usable by a caller that never
  imports `internal/policy` — proven both by the empirical experiment
  recorded in ADR 0008 and by this package's own end-to-end test using
  `trustvian.WithPolicy` without naming `policy.Policy` anywhere in the
  test file.
- No new dependency added anywhere; `go.mod`/`go.sum` unchanged.
- `go list -deps` confirms `config` does not import `net/http` or any
  OTel/Collector/provider package, and that the core detection engine
  (`event` through `internal/policy`, `Engine`) does not import
  `config`.
- `internal/policy` remains internal — no type promoted.
- `gofmt -l .`, `go vet ./...` clean.
