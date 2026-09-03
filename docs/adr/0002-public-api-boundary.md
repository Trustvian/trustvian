# 0002 — Public API boundary: `event` is public, `Policy`/`Config` stay internal

## Context

Go's `internal/` import restriction is the encapsulation mechanism
this module relies on instead of a Java/.NET-style `pkg/` wrapper
layer (CLAUDE.md: avoid unnecessary abstractions). The question is
*which* types must sit outside `internal/` for the public SDK
(`Engine.Analyze`/`Observe`) to actually be usable by a separate Go
module.

This surfaced as a real bug during initial implementation: `Event`
originally lived under `internal/event`. Since `internal/` blocks
importing the package from outside the module, and `Event` is the one
type every caller must *construct* just to call `Analyze` at all, this
made the SDK's own entry point uncallable by any external consumer —
discovered only when composing the `Engine` and writing its first
external-module-style test.

## Decision

Only two packages are importable from outside this module: the root
`trustvian` package (`Engine`, `Option`, `Result`) and `event`
(`Event`, `Actor`, `Operation`, `Target`, `Context`). Every other type
— `policy.Policy`/`Rule`/`Condition`/`Decision`, `anomaly.Config`,
`trust.Config`, `store.Store` — stays under `internal/`.

The line is: does an external caller need to *construct* a value of
this type to use the public API, or does it only ever *read* one
that's already been handed back on a `Result`? Go's `internal/`
restriction blocks importing a package, not reading exported fields
off a value you already hold — `result.Trust.Score` and
`result.Decision == "block"` both work from outside the module without
importing anything beyond the root package. So only types that are
*input* to the public API (starting with `Event`) need to be public;
types that only ever appear as *output* fields on `Result` do not.

Verified empirically, not just argued: a genuine external module (a
separate `go.mod`, using `replace` to point at this repository) was
built and run against `NewEngine()`/`Analyze()`/`Observe()` with no
code inside this repository — it compiles and runs. The same
experiment confirmed the flip side: that external module could not
construct a `policy.Policy` to pass to `WithPolicy`, since `Policy`'s
type lives under `internal/`.

## Alternatives considered

- **Promote `Policy`/`Config`/`Store` to a public package now**, "for
  consistency" with `Event`. Rejected: no external consumer needs this
  yet — the only current consumers of these options are this module's
  own tests and `cmd/trustvian`, both in-module and unaffected by the
  restriction. Promoting speculatively is exactly the kind of
  abstraction added without a real consumer that CLAUDE.md says to
  avoid, and it's a one-way door: once a type is public API, removing
  it is a breaking change.
- **A `pkg/` directory for everything meant to be public.** Rejected:
  `internal/` already provides the encapsulation; a parallel `pkg/`
  layer adds ceremony without a Go-idiomatic justification.

## Consequences

- External customization of policy/thresholds/storage is not possible
  today from a separate module — documented explicitly in
  [README.md § Limitations](../../README.md#limitations) and
  [Go SDK Guide § the public/internal boundary today](../sdk-guide.md#the-publicinternal-boundary-today)
  rather than left as a silent gap.
- When an external consumer's need is real (not hypothetical), the
  fix is additive: move the needed type(s) to a public package. No
  existing internal package needs to change to support that move,
  since the dependency direction already treats those types as leaves
  relative to `internal/otel`-style adapters.
