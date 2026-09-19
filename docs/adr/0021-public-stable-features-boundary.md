# 0021 — A public stable-features view for the context-risk callback

**Status:** Accepted

## Context

`WithContextRisk` took `func(features.StableFeatures) float64`. The
parameter type lives under `internal/`, so a third-party module could
not write a function of that signature — Go refuses the import outright.
The option was exported and uncallable from outside this module.

This is the same gap [ADR 0008](0008-policy-config-boundary.md) closed
for `policy.Policy` and [ADR 0017](0017-public-anomaly-configuration-boundary.md)
closed for `anomaly.Config`, with one difference that rules out their
solution. Those options take a *value the caller supplies*, so a public
configuration document plus a compiler was enough. A callback is the
reverse direction: the engine hands the caller a value, and the caller
must be able to name its type to declare the function at all. There is
nothing to compile.

The v1.0 public API review found this and `WithTrustConfig` to be the
only two exported options with no external path. `WithTrustConfig`
followed the established facade pattern (`config.TrustConfig` +
`CompileTrust`). This one needed a decision.

## Decision

Add `trustvian.StableFeatures`, a public value type carrying the six
stable dimensions, and change `WithContextRisk` to take
`func(StableFeatures) float64`. The engine projects its internal
representation into the public one at the callback boundary.

**Public, not promoted.** `internal/features.StableFeatures` stays
internal and keeps evolving freely. The public type is a separate
declaration that happens to carry the same six fields today. Feature
*derivation* remains in `internal/features` — this is a projection at
the boundary, not a second implementation, so there is one source of
truth for how features are computed.

**The root package, not `event`.** `event` is documented as the input
domain: the shape every pipeline stage consumes, and the types a caller
constructs to call `Analyze`. Stable features are neither — they are
derived, and the caller never builds one. That is precisely `Result`'s
relationship to the engine, and `Result` lives in the root package. The
type sits where it is produced and consumed.

**Deliberately only the stable dimensions.** The callback sees actor
type, operation category and name, target name and category, and
environment. It does not see latency, timestamps, error state, session,
or delegation. A context risk is a statement about a *kind* of
operation — "reaching the secrets manager is inherently sensitive" —
and how unusual one particular occurrence is already has a mechanism:
the anomaly stage. Widening this view would create a second, parallel
path for per-event scoring that nothing asked for.

Every field is a value type of an already-public type or a string, so
the public type's own type graph introduces no new public surface
beyond itself.

## Alternatives considered

- **Pass `event.Event` to the callback.** Rejected. It is the largest
  possible widening — session, delegation, attributes, raw timestamps —
  for a function whose job is to classify a kind of operation. It would
  also let a context-risk function silently become a second anomaly
  scorer, with none of the anomaly stage's explainability.
- **Promote `internal/features.StableFeatures`.** Rejected. It would
  freeze an internal representation for the life of `v1`, including any
  field added later for a reason having nothing to do with this
  callback. The public view is a contract; the internal struct is an
  implementation.
- **Put the type in `event`.** Rejected on the semantics above:
  `event` holds what a caller constructs, and this is derived.
- **Unexport `WithContextRisk`.** Rejected. Context risk is a genuine
  capability — the one signal an operator can assert that learning
  cannot erode — and removing it to avoid a boundary decision would be
  the wrong trade.
- **Leave it in-module only and document that.** Rejected: shipping
  `v1.0` with a documented, exported option that no consumer can call
  is worse than either fixing it or removing it.

## Consequences

- `WithContextRisk`'s signature changed. This is a source-breaking
  change to an exported function, made deliberately before the `v1`
  freeze rather than after it, when it would have required a major
  version. No in-repository caller used the option.
- The public type must be kept in step with the internal one whenever a
  stable dimension is genuinely added — an additive, minor-version
  change under the compatibility contract, and a deliberate decision
  each time rather than an automatic propagation.
- Every exported `Engine` option is now callable from a third-party
  module, proven by `examples/configured-engine`, which is a separate
  Go module and therefore cannot import `internal/` at all.
- Behavior is unchanged. The default callback still returns 0, and a
  configured callback receives the same six values it would have
  received before.
