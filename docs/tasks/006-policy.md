# 006 — Policy Engine Hardening

**Milestone:** v0.1 · **Depends on:** none · **Blocks:** none

## Objective

Close the one concrete, spec-named gap in `internal/policy.Condition`
— it cannot match on arbitrary event attributes (the original spec's
own `tool.category: secrets` policy example needs exactly this) —
without building a general-purpose policy language.

## Why

`Condition` already matches on `ActorType`, `OperationCategory`,
`TargetName`, `Environment`, and `MinRiskLevel` — a deliberately flat
AND-of-optional-fields matcher (see
[Policy Guide § the types](../policy-guide.md#the-types)). This
already covers most realistic policies (see the CLI's own default
policy and every [use-cases.md](../use-cases.md) example). What it
cannot do: match on a specific `Event.Attributes` key/value pair,
which is exactly what the original spec's AI-agent policy example
needs (`tool.category: secrets`) and what
[`internal/policy/policy.go`](../../internal/policy/policy.go)'s own
`Condition` doc comment already names as a known, deliberate,
documented gap.

## Scope

- Add a minimal attribute matcher to `Condition` — e.g. an
  `Attributes map[string]string` (or a small `[]AttributeMatch{Key,
  Value}` slice) field, ANDed with the existing fields exactly like
  every other `Condition` field today. Values compare as strings
  (matching JSON's natural representation) or via a small, explicit
  type switch for the common `Attributes` value types
  (`string`/`bool`/numeric) — decide the simplest option that closes
  the spec's example during implementation; do not build general
  comparison operators (`>`, `<`, regex, etc.) — that's the "complex
  policy language" the roadmap brief explicitly says not to build yet.
- `policy.Input` gains the `Attributes` needed for this — likely
  `event.Event.Attributes` itself or a `features.Features`-adjacent
  carrier; decide the minimal plumbing change through `Engine.Analyze`
  during implementation, keeping `Input`'s existing fields
  (`Stable`, `Trust`) unchanged in meaning.

## Non-Goals

- No AND/OR/NOT combinators between `Condition`s — `Rule.When` stays a
  single flat `Condition`; composing multiple conditions with boolean
  logic is exactly the "complex policy language" this task must not
  build. If a real need for combinators emerges later, it gets its own
  roadmap task with its own justification.
- No dynamic policy loading (YAML/file-based) — `Policy` stays a Go
  value literal; [ADR entries] already establish this is intentionally
  deferred (see [`internal/policy/policy.go`](../../internal/policy/policy.go)'s
  package doc).
- No policy versioning — named in the original spec's Phase 3 but not
  justified by any current milestone; stays in
  [ROADMAP.md](../ROADMAP.md)'s "planned, not scoped" bucket.

## Technical Requirements

- The new matcher must not change `Condition{}`'s zero-value behavior
  (still matches everything) — an empty/nil `Attributes` map means
  "don't care," consistent with every other field.
- `Policy.Evaluate`'s fail-closed guarantee
  ([SECURITY.md § policy bypass](../SECURITY.md#policy-bypass)) is
  unaffected — this task only adds a new way for `When`/`Unless` to
  match, not a new way to reach a `Decision`.
- Must not require importing `internal/otel` or any adapter-specific
  type — `Attributes` is already a plain `map[string]any` on
  `event.Event`, no new coupling.

## Tests

- Extend `internal/policy/policy_test.go`'s existing
  `TestConditionMatchesPerField` table with attribute-matching cases:
  match, mismatch, absent key, empty/nil matcher (matches everything).
- A rule using the new matcher reproducing the spec's own example
  (`ActorType: ai_agent` + an attribute matcher for a secrets-like
  category) end to end through `Policy.Evaluate`.
- Confirm existing tests (rule ordering, `Unless`, all three
  fail-closed scenarios) are unaffected — no test should need to
  change shape, only the new ones are additive.

## Benchmarks

- Re-run `BenchmarkEvaluateMatch`/`BenchmarkEvaluateDefault`
  (`internal/policy`) after the change; a `Condition` with no
  attribute matcher set (the common case today) must stay
  zero-allocation, matching the existing measured baseline (21–37
  ns/op, 0 allocs/op — see [PERFORMANCE.md](../PERFORMANCE.md)).

## Documentation

- [Policy Guide](../policy-guide.md): update the `Condition` field
  reference table; replace the "attribute-based conditions... need a
  future policy loader" caveat with the actual usage example, or note
  precisely what's still deferred (combinators, dynamic loading) if
  anything remains.
- [DOMAIN.md § Policy and Decision](../DOMAIN.md#policy-and-decision):
  minor update if `Input`'s shape changes.

## Acceptance Criteria

- `go test ./internal/policy/... -race` green.
- The spec's own `tool.category: secrets`-style example is
  reproducible as a real, tested `Rule`.
- Zero-allocation common-case benchmark numbers unchanged.
- No new combinator/boolean-logic surface added to `Condition`.
