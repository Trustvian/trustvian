# 0008 — `config` is public; `internal/policy` stays internal

## Context

[Task 019](../tasks/019-policy-config-model.md) (`v0.5`, Policy &
Configuration) exists to close a gap [ADR 0002](0002-public-api-boundary.md)
named: no code outside this module can construct a `policy.Policy`,
because `Policy`/`Condition`/`Rule`/`Decision` all live under
`internal/policy`. This is not theoretical — `processor/`, a
genuinely separate Go module, resolves every span to `observe_only`
today for exactly this reason (see
[`processor/README.md` § Configuration](../../processor/README.md#configuration)).

[ADR 0007](0007-alert-package-is-public.md) already answered a similar
question for `alert`: promote a type to a public package specifically
when a real external consumer needs to *construct* one, and only
promote the minimum needed. The obvious first guess for `config` was
to apply the identical fix: promote `policy.Policy`/`Condition`/
`Rule`/`Decision` themselves to a public package (or move them
wholesale, as CLAUDE.md's own instructions for this task explicitly
warned against doing "unless an existing ADR explicitly requires it").
Before doing that, this task tested a narrower alternative.

## Decision

**`internal/policy` is not touched. A new public package, `config`,
defines its own primitive-typed structs (`PolicyConfig`, `PolicyRule`,
`PolicyCondition`) and one function, `CompilePolicy(cfg PolicyConfig)
(policy.Policy, error)`, that validates and translates them into a
real `policy.Policy`.**

This works — for a genuinely external caller, not just for code inside
this module — because of a Go language property verified empirically,
not assumed: **a value of an internal-to-you type can be received from
an exported function and passed into another exported function that
explicitly declares that type as its parameter, entirely via type
inference, as long as the caller's own source never spells out the
type's name.** Go's `internal/` visibility rule blocks *importing* a
package; it does not block holding or forwarding a value whose type
happens to be defined in that package.

Verified concretely: a throwaway function
`ZZZScratchDefaultPolicy() policy.Policy` was added temporarily to the
root package, and a real, separate scratch Go module (its own `go.mod`,
a `replace` directive back to this repository — the same shape
`examples/`, `processor/`, and every ADR 0002 verification already
uses) called it, received a `policy.Policy` value via `:=`, and passed
that value directly into the real `trustvian.WithPolicy(p)` — compiling
and running successfully with zero `internal/` import anywhere in the
scratch module's source. The throwaway function was deleted
immediately after. This is the same experimental-verification
discipline ADR 0002 itself used ("a genuine external module... was
built and run against `NewEngine()`/`Analyze()`/`Observe()`") applied
to this specific question.

`CompilePolicy` is therefore the public boundary `config` needs to
provide — not a public `Policy` type. `PolicyConfig`/`PolicyRule`/
`PolicyCondition` are new, `config`-owned types (not aliases or
promotions of `internal/policy` types), built entirely from primitive
Go types (`string`, `map[string]string`, slices of the above) so that
constructing one — by hand in Go today, from a parsed config file once
task 019's own follow-up tasks add a loader — never requires importing
`internal/policy`, `internal/trust`, or `event` either.

## Alternatives considered

- **Promote `policy.Policy`/`Condition`/`Rule`/`Decision` to a public
  package** (either in place, `policy` at the root, or moved under
  `config`). Rejected: it is strictly more public surface than the
  problem requires. Every one of `processor/`'s, the CLI's, and a
  future standalone deployment's actual needs — construct a `Policy`
  from declarative input, hand it to `WithPolicy` — is satisfied by
  `CompilePolicy` alone. Promoting the runtime types as well would
  additionally commit this project to `internal/policy`'s exact
  current shape (field names, `Condition`'s exact matcher set) as
  public API forever, for no capability gain. This is exactly the
  "narrow public API, not internal implementation exposed for
  convenience" principle task 019's own brief states.
- **Rename/move `internal/policy` to a public `policy` package.**
  Rejected for the same reason, more bluntly: task 019's own explicit
  instruction is "do not simply move `internal/policy` to `policy`
  unless an existing ADR explicitly requires it" — no ADR does, and
  this one deliberately does not become that ADR.
- **Type `PolicyCondition`'s fields as the internal/public domain
  types directly** (`ActorType event.ActorType`, `MinRiskLevel
  trust.RiskLevel`) instead of plain `string`. Considered and rejected
  for uniformity: `event.ActorType` is already public and would have
  worked, but `trust.RiskLevel` is internal, and a mixed
  public-type/internal-type/plain-string field set on one struct would
  be a confusing, inconsistent public contract. Plain strings for every
  matcher field, validated against the real enums internally by
  `Validate`, keep `PolicyCondition`'s exported shape uniformly
  primitive and trivially serializable once a config-file loader
  exists.
- **Have `config` re-implement `Condition.Matches`' evaluation logic**
  so it could operate independently of `internal/policy`. Rejected:
  this would duplicate the one thing `internal/policy` is trusted to
  get right, and would let `config`'s and `internal/policy`'s matching
  semantics drift apart over time. `CompilePolicy` only ever
  *translates* field values; `policy.Condition.Matches` remains the
  sole place matching is evaluated.

## Consequences

- `internal/policy` gains zero new public-compatibility obligations
  from this task — it can still evolve its internal field names or
  matching internals freely, as long as `CompilePolicy`'s translation
  keeps working, exactly the freedom ADR 0002's original reasoning
  described.
- `config`'s own exported shapes (`PolicyConfig`, `PolicyRule`,
  `PolicyCondition`, `SchemaVersionV1`, `CompilePolicy`, `Validate`)
  now carry the same compatibility discipline
  [CHANGELOG.md § Public API compatibility
  promise](../../CHANGELOG.md#public-api-compatibility-promise)
  already documents for `event`/`trustvian`/`alert` — this is the
  fourth public package, and additions to it are a real, durable
  contract from this point on.
- A future file-format loader (task scoped separately, once real usage
  informs it) only ever needs to produce a `config.PolicyConfig`
  value — it does not need its own path into `internal/policy`, and
  neither does a future `processor/` integration or CLI flag. Every
  future consumer converges on the same `CompilePolicy` boundary.
- If `internal/policy.Condition` ever grows a new matchable dimension
  (e.g. a numeric anomaly-score/trust-score threshold — explicitly not
  added by this task), `config.PolicyCondition` and `CompilePolicy`
  gain a corresponding new field and translation line as a purely
  additive change; no boundary decision from this ADR needs to be
  revisited to do that.
