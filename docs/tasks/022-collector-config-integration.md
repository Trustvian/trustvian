# 022 — OTel Collector Policy Configuration Integration

**Milestone:** v0.5 · **Depends on:** [019](019-policy-config-model.md)
and [020](020-policy-config-loader.md) (the public `config` package
this task consumes, unmodified) · **Does not depend on:** [021](021-cli-config-integration.md)
(CLI integration; independent consumer of the same `config` package) ·
**Blocked on:** a real, pushed Trustvian core release exposing
`config` — see § Release / Module Compatibility below. This is the one
task in this sequence whose acceptance criteria could not be fully
closed within this task's own scope, for a reason external to the code
itself.

## Objective

Let the standalone OTel Collector processor module
([`processor/`](../../processor/)) declare a real Trustvian `Policy`
from Collector configuration, via the exact same `config.PolicyConfig`
→ `config.CompilePolicy` → `trustvian.WithPolicy` path the Go SDK and
the CLI (task 021) already use — without inventing a second policy
model inside the processor, without exposing `internal/policy`, and
without redesigning the processor.

## Why

`processor/README.md` § Configuration has said, since task 009, that
this processor "always runs Trustvian's default Engine configuration"
because `WithPolicy` took an `internal/policy` type no separate module
could construct. Task 019 closed that gap for every consumer *except*
this one — `processor/` never got a corresponding update. This task is
that update.

## Scope

```text
Collector YAML (`processors: trustvian: policy: ...`)
      ↓  Collector's own confmap decoder
Config.Policy (map[string]any)
      ↓  decodePolicy (go-viper/mapstructure/v2, TagName "yaml")
config.PolicyConfig
      ↓  config.CompilePolicy (validates + compiles)
policy.Policy
      ↓  trustvian.WithPolicy
Engine  (constructed once, at createTracesProcessor time)
      ↓
ConsumeTraces / span processing (unchanged)
```

- `processor.Config` gains one new field, `Policy map[string]any`
  (`mapstructure:"policy,omitempty"`) — see `processor/config.go`.
- `decodePolicy(map[string]any) (config.PolicyConfig, error)`
  (`processor/config.go`) converts that generic map into a real
  `config.PolicyConfig`, using `go-viper/mapstructure/v2` (already in
  this module's dependency graph, transitively via
  `go.opentelemetry.io/collector/confmap`) configured with
  `TagName: "yaml"` — reading `config.PolicyConfig`'s own existing
  `yaml:"..."` struct tags (added by task 020 for its file loader) as
  the field-mapping source. See § Why not embed `config.PolicyConfig`
  directly below for why this indirection is necessary, not
  incidental complexity.
- `newTrustvianProcessor` (`processor/processor.go`) becomes fallible:
  `(*trustvianProcessor, error)`. `cfg.Policy == nil` (no `policy:`
  block at all) keeps its exact prior behavior —
  `trustvian.NewEngine()` with no options. `cfg.Policy != nil`
  (including an explicit, empty `policy: {}`) is decoded, validated,
  and compiled; any error there is returned, never swallowed.
- `createTracesProcessor` (`processor/factory.go`) propagates that
  error, failing the whole Collector's startup — this function runs
  during pipeline graph construction, before `Start`, so an invalid
  explicit policy never reaches a running pipeline or a first span.

## Why not embed `config.PolicyConfig` directly

Section 7 of this task's own brief (and the general "reuse the
canonical model" instinct) suggested
`type Config struct { Policy config.PolicyConfig `mapstructure:"policy"` } }`.
Verified empirically before writing any processor code (matching this
repository's own "verify, don't assume" discipline — see ADR 0008)
that this does not work:

`go.opentelemetry.io/collector/confmap`'s decoder (`internal.Decode`)
configures its underlying `mapstructure.Decoder` with
`TagName: "mapstructure"` and, critically, `MatchName:
caseSensitiveMatchName` — an exact-string, case-sensitive comparison,
overriding mapstructure's own default case-*insensitive* fallback.
`config.PolicyConfig`'s fields only carry `yaml:"..."` tags (`version`,
`default_decision`, `min_risk_level`, etc.) — Collector's decoder never
looks at those, and an untagged field falls back to its literal Go
identifier (`Version`, `DefaultDecision`), which the case-sensitive
matcher never equates with a lowercase, underscored YAML key like
`default_decision`. Confirmed directly: decoding
`{"policy": {"version": "v1", "default_decision": "observe_only", ...}}`
into an embedded, untagged `config.PolicyConfig` leaves every
snake_case field at its zero value (or fails outright with an
"unused key" error, since Collector's decoder defaults to
`ErrorUnused: true`).

The one clean fix — add `mapstructure:"..."` tags to
`config.PolicyConfig`/`PolicyRule`/`PolicyCondition` in the *root*
module — is exactly the kind of change that requires a new Trustvian
core release before `processor/` could depend on it, and this task
discovered (see § Release / Module Compatibility) that no release
newer than `v0.4.0` has actually been pushed to `origin`. Rather than
block this entire task on that release, `decodePolicy` reuses
`config.PolicyConfig`'s *existing*, already-released `yaml` tags by
pointing `go-viper/mapstructure/v2`'s own configurable `TagName` at
`"yaml"` directly (bypassing Collector's `confmap` wrapper for just
this one nested field, once Collector has already handed the
processor a plain `map[string]any`) — zero changes to the root module,
zero new dependency (the library is already in this module's graph),
and, critically, it does **not** create a second policy model:
`decodePolicy` performs no validation, no defaulting, and no
evaluation of its own. `config.CompilePolicy` remains the sole place
`PolicyConfig` is validated and compiled, identical to the Go SDK and
the CLI.

## Release / Module Compatibility — the one open item

**Discovered during this task, not assumed:** `processor/go.mod`
requires `github.com/Trustvian/trustvian v0.3.0` — a version that
predates the `config` package entirely (`config` was added in a commit
after `v0.4.0`). Checking which Trustvian tags are actually reachable
from `processor/go.mod`'s perspective —

```
$ git ls-remote --tags origin
...v0.1.0, v0.2.0, v0.3.0, v0.4.0 only
$ cd processor && go get github.com/Trustvian/trustvian@v0.5.0
go: github.com/Trustvian/trustvian@v0.5.0: invalid version: unknown revision v0.5.0
$ go mod tidy   # (with the new config import already in source)
go: downloading github.com/Trustvian/trustvian v0.4.0
go: trustvian-processor imports github.com/Trustvian/trustvian/config:
    module found (v0.4.0), but does not contain package .../config
```

— confirms a `v0.5.0` **git tag exists locally, but has never been
pushed to `origin`** (`github.com/Trustvian/trustvian`, the real
module path). It is not a real release: the Go module system (and
therefore any external `go get`/`go mod tidy`) cannot resolve it. The
latest version that genuinely exists on `origin` is `v0.4.0`, which
does not contain the `config` package at all.

This code (`processor/config.go`'s `decodePolicy`, and the
`Config.Policy`/`newTrustvianProcessor`/`createTracesProcessor`
changes) is implemented and fully tested against the current, local,
*unreleased* root module — using a local, git-ignored `go.work` at the
repository root (`use (. ./processor ./examples)`), exactly the
"local development may use `go.work`" allowance this task's own brief
names. It is **not** wired into `processor/go.mod`'s committed
`require` line: doing so today would either point at a genuinely
unresolvable version (`v0.5.0`, not on `origin`) or silently regress to
`v0.4.0` (which lacks `config` and would fail to build at all) — both
are exactly the "invalid dependency contract" this task's own brief
says release integrity outweighs. `go.work`/`go.work.sum` are
git-ignored (already listed in `.gitignore` before this task) and were
not committed.

**Required action, outside this task's own authority to perform**
(this session may not commit, push, or tag a release): push a real
Trustvian core release to `origin` that contains the `config` package
— either push the existing local `v0.5.0` tag once its content is
confirmed final, or cut a fresh tag (e.g. `v0.5.1`) — then, as a small,
separate follow-up change, update `processor/go.mod`'s `require
github.com/Trustvian/trustvian` line to that real, resolvable version
and run `go mod tidy`. Only after that follow-up lands does this
task's code become genuinely buildable outside a local workspace.

## Non-Goals

- No `processor.PolicyConfig`/`PolicyRule`/`PolicyCondition` types —
  `decodePolicy` produces a real `config.PolicyConfig` directly; no
  parallel model exists at any point.
- No Alert configuration (`alerts:`, `sinks:`, `webhook:` in Collector
  config) — a separate, future v0.5 task.
- No CLI changes — task 021 is unmodified.
- No new `policy.Condition`/`PolicyCondition` matchable dimensions.
- No hot reload / dynamic policy replacement — the Policy compiles
  once, at `createTracesProcessor` time, and is fixed for the
  processor instance's lifetime, exactly like every other
  Collector-config-driven component.
- No exported Collector-convention metrics for the observability
  counters `Stats()` already tracks (pre-existing non-goal, unchanged).

## Technical Requirements

- `cfg.Policy == nil` (the `policy:` key entirely absent) is
  byte-for-byte the pre-task default: `trustvian.NewEngine()`, no
  options — proven by
  `TestConsumeTracesDefaultPolicyUnchangedWithoutConfig` and every
  pre-existing processor test passing unmodified.
- `cfg.Policy != nil` (present, even as an empty `policy: {}`) is
  treated as an explicit request: decoded, validated, compiled, and
  any failure anywhere in that chain is returned from
  `createTracesProcessor` — no processor instance is returned, no
  fallback to the default Policy. Proven by
  `TestNewFactoryFailsOnInvalidPolicy` and
  `TestConfigUnmarshalEmptyPolicyBlockIsNonNil`.
- `createTracesProcessor`'s error path returns a bare `nil`, not
  `newTrustvianProcessor`'s own typed `(*trustvianProcessor)(nil)`
  forwarded directly — converting a nil concrete pointer into the
  `processor.Traces` interface return value produces a *non-nil*
  interface (Go's typed-nil-in-interface trap), which would make a
  caller's `proc != nil` check wrongly pass on a failed construction.
  Caught by `TestNewFactoryFailsOnInvalidPolicy` itself during this
  task (it failed before this fix was applied — see the task's own
  commit history / session log for the concrete before/after).
- Rule order from the Collector config's `rules:` YAML sequence
  survives decoding and compilation unchanged (first-match-wins) —
  proven by `TestConsumeTracesPolicyRuleOrderingFirstMatchWins`.
- Policy compiles exactly once, at `createTracesProcessor` time, never
  per span — `processSpan`/`ConsumeTraces` are unmodified by this
  task; `BenchmarkConsumeTraces` shows no change in the per-span
  allocation/latency shape (compile-once behavior is structural, not
  something a benchmark number alone would catch a regression in, but
  it confirms nothing new leaked into the hot path).

## Tests

`processor/config_test.go` (new):
`TestConfigUnmarshalDecodesPolicyBlock`,
`TestConfigUnmarshalOmittedPolicyLeavesNilMap`,
`TestConfigUnmarshalEmptyPolicyBlockIsNonNil` — all exercise the real
`confmap.Conf.Unmarshal` path (Collector's actual decoder, not a
hand-rolled substitute), proving the nil/empty-map distinction
`newTrustvianProcessor` relies on actually holds.

`processor/factory_test.go` (+1): `TestNewFactoryFailsOnInvalidPolicy`
— an explicit, invalid `policy:` block fails `CreateTraces` with a
non-nil error and a nil processor.

`processor/processor_test.go` (+3):
`TestConsumeTracesConfiguredPolicyChangesDecision` (the central
acceptance test — a real span, through a real configured Policy,
ends up with `trustvian.decision = "block"` where the default Policy
would have produced `observe_only`),
`TestConsumeTracesDefaultPolicyUnchangedWithoutConfig` (regression: an
omitted `policy:` still produces `observe_only`), and
`TestConsumeTracesPolicyRuleOrderingFirstMatchWins`.

All pre-existing processor tests pass unmodified. Full suite:
`go test ./... -race` green across every test in the module.

## Documentation

- [processor/README.md](../../processor/README.md) § Configuration:
  updated — the processor now supports a real `policy:` block; the
  "every span resolves to observe_only" statement is now conditional
  on omitting it.
- [docs/OPENTELEMETRY.md](../OPENTELEMETRY.md) § The OTel Collector
  processor: updated to reflect the new configuration surface, still
  correctly scoped as "this Collector-config decoding path, not
  `internal/otel`'s own adapter."
- [docs/policy-guide.md](../policy-guide.md): updated to mention the
  Collector processor now consumes the same schema, alongside the
  CLI's `--config` and the Go SDK's `config.LoadFile`.
- [README.md](../../README.md): "no OTel Collector processor
  integration yet" language corrected; the release-gap caveat above is
  carried into the Limitations section, since it is genuinely still
  true from an end user's perspective (they cannot `go get` a
  Trustvian core version containing `config` yet).
- [docs/ROADMAP.md](../ROADMAP.md): this task marked done under `v0.5`
  for its own scope (the processor code, tests, and docs); the release
  gap it depends on to actually ship is called out explicitly, since
  it is a real blocker for `v0.5`'s overall completion independent of
  any remaining task scope.

## Acceptance Criteria

- `go build`/`go vet`/`gofmt -l`/`go test ./... -race -count=1` green
  in `processor/`, using a local `go.work` for now (see § Release /
  Module Compatibility).
- A real Collector processor config with a `policy:` block produces a
  different `trustvian.decision` than the same config without one, on
  the same span — demonstrated by an automated test exercising the
  real `confmap`/factory/`ConsumeTraces` path end to end.
- An invalid, explicit `policy:` block fails `CreateTraces` (Collector
  startup) outright, with no processor instance returned — demonstrated
  the same way.
- No `processor.PolicyConfig`/`PolicyRule`/`PolicyCondition` type
  exists anywhere in the module.
- `processor/` does not import `internal/policy`.
- `go.mod`/`go.sum` diff at the repository root: empty. `processor/go.mod`'s
  `require github.com/Trustvian/trustvian` line: unchanged at `v0.3.0`
  (see § Release / Module Compatibility for why bumping it today would
  itself be the defect this criterion exists to prevent).
- **Not yet closable within this task:** `processor/go.mod` actually
  declaring and resolving a dependency on a released Trustvian version
  containing `config` — blocked on a release outside this task's (and
  this session's) authority to perform.
