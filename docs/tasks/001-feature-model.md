# 001 — Feature Model Hardening

**Milestone:** v0.1 · **Depends on:** none (extends existing
`internal/features`) · **Blocks:** [002](002-fingerprint.md) if the
stable-dimension set changes

## Objective

Formally validate the current stable/volatile classification against
a concrete, complete list of behavioral signals, and close the one
genuine gap: `Target` currently has no way to distinguish *what kind*
of destination it is (database vs. external API vs. internal service),
which limits how specific future anomaly signals (e.g.
"unusual target *category*", not just "unusual target *name*") can be.

## Why

`internal/features.Extract` already splits `Event` into stable
(`ActorType`, `OperationCategory`, `OperationName`, `TargetName`,
`Environment`) and volatile (`Timestamp`, `Latency`, `Error`)
dimensions — this is not being rebuilt. But the roadmap's own example
list of stable signals (actor identity/category, service, operation,
route, method, target *type*, target *category*, dependency,
environment) names a dimension — target type/category — that doesn't
exist today. Without it, `Fingerprint` can only ever say "this exact
destination is unfamiliar," never "destinations of this *kind* are
unusual for this actor" — a materially weaker signal for e.g. "an
actor that has only ever called internal services suddenly calls an
external one," which today only fires if the *specific* external host
is novel, not the category shift itself.

## Scope

- Audit `internal/features.StableFeatures`/`VolatileFeatures` against
  the roadmap's explicit examples; document the mapping (this is
  mostly confirmation, not new code — see Documentation below).
- Add an optional `Target.Category` field to `event.Event` (e.g.
  `internal`, `external`, `database` — small, closed enum, following
  the same pattern as `OperationCategory`). Optional: zero value means
  "unclassified," matching how `Direction` is already optional today.
  A producer (JSON, OTel adapter, or direct SDK use) may set it or
  leave it unset.
- Include `Target.Category` in `StableFeatures` when adding it, so it
  flows into `Fingerprint`/`Anomaly` automatically once 002 accounts
  for the new dimension.
- Confirm (with a test, not just by inspection) that `Context.TraceID`,
  `Context.SpanID`, and `Event.ID` never influence `Fingerprint.ID` —
  they already don't, per `fingerprint.Compute`'s five-field hash, but
  this should be an explicit regression test, not an implicit
  guarantee.

## Non-Goals

- No change to what's already volatile (`Timestamp`, `Latency`,
  `Error`) — these are correctly excluded from `Fingerprint` today.
- No dependency-graph tracking (a full "service dependency graph"
  concept) — `Target.Name`/`Category` is sufficient for now; a richer
  dependency model is not justified by any current milestone.
- No change to `OperationCategory`'s existing five values.

## Technical Requirements

- `Target.Category` (or equivalent name, decided during implementation)
  is a new `event.TargetCategory` string type with a small closed set
  of constants, following the exact pattern of `event.OperationCategory`
  (see `event/event.go`).
- Zero value must mean "unclassified" and must not fail `Event.Validate()`
  — it's optional, matching `Target` and `Direction`'s existing
  optionality.
- `features.StableFeatures` gains a `TargetCategory` field, populated
  by `Extract`.
- No change to `features.Extract`'s allocation profile for events that
  don't set the new field (it should stay the documented
  zero-allocation common case — see
  [PERFORMANCE.md](../PERFORMANCE.md)).

## Tests

- Table-driven tests in `event/event_test.go` for the new type's
  validity (mirrors `ActorType`/`OperationCategory` test patterns).
- `internal/features/features_test.go`: `TargetCategory` extraction,
  including the case where it's unset.
- A new explicit regression test proving `Context.TraceID`/`SpanID`/
  `Event.ID` changes never change `Fingerprint.ID` (currently true by
  construction but not directly asserted end-to-end from `Event`
  through to `fingerprint.Compute`).

## Benchmarks

- Re-run `BenchmarkExtract` (`internal/features`) after the change;
  the common case (no `TargetCategory` set) must stay 0 allocs/op — a
  regression here blocks this task, per CLAUDE.md's hot-path
  requirement.

## Documentation

- [DOMAIN.md § Feature](../DOMAIN.md#feature): update the stable-feature
  list to include `TargetCategory`.
- [ARCHITECTURE.md](../ARCHITECTURE.md): no structural change expected
  (same package, same dependency direction) — confirm and note "no
  change" rather than skip the check.

## Acceptance Criteria

- `go test ./event/... ./internal/features/...` green, including the
  new tests.
- `BenchmarkExtract`'s zero-allocation common case is unchanged
  (verified by benchmark, not assumed).
- `DOMAIN.md` accurately lists the new stable dimension.
- No change to `event.Event`'s required-field `Validate()` behavior —
  existing valid events remain valid without modification.
