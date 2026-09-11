# 0009 — Alert configuration is a separate document from Policy configuration

## Context

[Task 023](../tasks/023-declarative-alert-configuration.md) (`v0.5`,
Policy & Configuration) closes the last named gap in that milestone:
`alert.Rule` — like `policy.Policy` before [task
019](../tasks/019-policy-config-model.md) — can only be constructed by
code living inside this module. The obvious question this task had to
answer first: does the existing, released `PolicyConfig` document grow
an `alerts:` section, or does Alert configuration get its own,
separate document and type?

`trustvian-project-spec.md` § 17.4 itself anticipated this question,
illustratively, before either config model was implemented: "the same
loader and file format may express both \[Policy and Alert
configuration\] — but they remain separate runtime concepts." That
section is explicitly "illustrative only... not a committed DSL,"
written before task 019 fixed `PolicyConfig`'s real, released shape
(a flat document — `version`/`default_decision`/`default_reason`/
`rules` at the top level, no `policies:` wrapper, unlike the spec's own
illustrative `policies:` list syntax). Task 019/020's actual schema v1
is already a real, tagged (locally) release surface; any change to its
document shape is a backward-compatibility decision this ADR has to
make deliberately, not one the spec's older illustrative text can
settle by itself.

## Decision

**Alert configuration is a new, independent public type,
`config.AlertConfig`, with its own schema version
(`AlertSchemaVersionV1`), its own compiler (`CompileAlerts`), and its
own loader (`LoadAlerts`/`LoadAlertsFile`) — not a new field on
`PolicyConfig`, and not a combined "schema v2" document wrapping both
`policy:` and `alerts:` as sibling keys.**

Concretely:

```text
PolicyConfig  --Validate-->  --CompilePolicy-->  policy.Policy
AlertConfig   --Validate-->  --CompileAlerts-->   []alert.Rule
```

Two independent documents, two independent public types, two
independent compilers — reusing the same package (`config`), the same
YAML decoding discipline (`go.yaml.in/yaml/v3`,
`Decoder.KnownFields(true)`, duplicate-key rejection, a bounded file
read via `maxConfigFileSize`), and the same validation conventions
(sentinel errors, first-error-wins, bounded rule count/name length),
but never merged into one schema or one Go type.

## Alternatives considered

- **Option A — introduce Config Schema v2**, wrapping both sections as
  siblings:

  ```yaml
  version: v2
  policy:
    default_decision: observe_only
    rules: [...]
  alerts:
    rules: [...]
  ```

  while `Load`/`LoadFile` keep accepting schema v1 (policy-only,
  unchanged) for backward compatibility. This is the shape §17.4's
  illustrative text gestures at, and it has a real advantage: one file,
  one `--config` flag, for an entire deployment's configuration.
  Rejected for `v0.5`, for three concrete reasons:

  1. **Return-type polymorphism.** `Load`/`LoadFile` return a single
     concrete type today (`PolicyConfig`). A schema-v2-aware loader
     needs to return *something* that can hold both a `PolicyConfig`
     and an `AlertConfig` depending on the document's own `version:`
     field — a new wrapper type (`Config{Policy *PolicyConfig, Alerts
     *AlertConfig}`) that `Load` would need to populate differently
     per version, or a second, version-branching entry point. Either
     shape is real, non-trivial surgery to an existing, tested,
     already-released (locally tagged `v0.5.0`) loader — risk this
     task has no forcing external consumer to justify yet.
  2. **No real consumer asked for one file yet.** Neither the CLI
     ([task 021](../tasks/021-cli-config-integration.md)) nor the
     Collector processor ([task
     022](../tasks/022-collector-config-integration.md)) wires Alert
     configuration in at all (see both tasks' own Non-Goals) — there is
     no `--alert-config` flag and no `alerts:` Collector block today.
     Building the combined-file plumbing before either consumer exists
     is exactly the speculative abstraction CLAUDE.md's "Don't design
     for hypothetical future requirements" warns against.
  3. **Schema v1 must stay untouched.** This task's own backward
     compatibility requirement — an existing `v1` `PolicyConfig` file
     must keep working, byte-for-byte, forever — is trivially,
     structurally guaranteed by *not touching* `Load`/`LoadFile`/
     `PolicyConfig` at all, rather than guaranteed by a v1/v2 branch
     inside a loader that now has more surface area to keep correct.

- **Rename/merge `PolicyRule`/`AlertRuleConfig` into one generic rule
  type.** Rejected outright — this is the one option this task's own
  brief calls "non-negotiable" to avoid. `policy.Rule` and `alert.Rule`
  already have materially different runtime contracts (mandatory
  default + fail-closed vs. no-match-means-no-alert; `Unless`
  suppression vs. none; a `Reason` string vs. none) that a merged
  config type would either have to paper over or leak through as
  always-optional fields with different meanings per consumer — the
  same kind of confusing, inconsistent public contract ADR 0008 already
  rejected for `PolicyCondition`'s own field typing.

## Consequences

- `PolicyConfig`, `Load`, `LoadFile`, and schema v1 are **completely
  unmodified** by this task — verified by running the full pre-existing
  `config` test suite unchanged (see task 023's own Tests section).
  There is no schema-version branch anywhere in this package as of
  `v0.5`.
- `config`'s public surface gains four new symbols
  (`AlertSchemaVersionV1`, `AlertConfig`, `AlertRuleConfig`,
  `AlertConditionConfig`) plus three new functions (`CompileAlerts`,
  `LoadAlerts`, `LoadAlertsFile`) — all carrying the same
  [CHANGELOG.md § Public API compatibility
  promise](../../CHANGELOG.md#public-api-compatibility-promise)
  `PolicyConfig`'s own symbols already carry.
- A future "one file for a whole deployment" schema v2 is not
  foreclosed by this decision — if a real consumer (a standalone
  runtime, a packaging format) eventually needs it, that future task
  can introduce a `Config{Policy, Alerts}` wrapper type and a new
  version-aware entry point additively, without this ADR needing to be
  reversed: `PolicyConfig`/`AlertConfig` themselves would be reused
  unchanged as that wrapper's two fields, exactly as
  `CompilePolicy`/`CompileAlerts` would be reused unchanged as its two
  compilation steps.
- The CLI and the Collector processor do not gain `--alert-config`/
  `alerts:` support from this decision by itself — that remains
  separate, future work per task 023's own Non-Goals, independent of
  which schema-evolution option was chosen here.
