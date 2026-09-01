# Security Principles

These follow directly from CLAUDE.md's "Security decisions must be
explainable, deterministic where possible, auditable, testable" and
"Never silently weaken a security policy" — this file is where those
apply to specific mechanisms already in the codebase.

## Fail closed, never silently open

`policy.Policy.Evaluate` is the last word on this: an unconfigured or
misconfigured `Policy` (empty/invalid `DefaultAction`, or a
`DefaultAction` set but `DefaultReason` left empty) does not fall
through to any particular `Decision` by accident — it explicitly
returns `BLOCK` with a fixed reason. There is no code path from "the
config is wrong" to a silent `ALLOW`. If you add a new way to
configure a `Policy` (a loader, a builder), it must go through the same
`Evaluate` validation, not bypass it.

## Learning is gated, not left to caller discipline

`Engine.Observe` decides for itself whether a `Result` is eligible to
be folded into the `Baseline` — it is not the caller's job to check
`Result.Decision` first. The line is whether the action *proceeded*:
`ALLOW`, `OBSERVE_ONLY`, and `ALERT` all let the event through (`ALERT`
only adds visibility) and are eligible; `CHALLENGE`, `REQUIRE_APPROVAL`,
and `BLOCK` all hold or stop the action and are excluded. This is what
closes the baseline-poisoning path: an actor cannot "wear down" the
system into trusting bad behavior by repeating it, because repeating a
blocked action never produces more learning-eligible observations.

Note `ALERT` is eligible on purpose, not `ALLOW`/`OBSERVE_ONLY` alone —
excluding it created a real deadlock (see `.claude/rules/testing.md`
and `eligibleForLearning`'s doc comment in `engine.go`): a brand-new,
entirely benign fingerprint can transiently cross into `ALERT`-level
risk purely from partial maturity, and if that state were ineligible
for learning, it could never mature past it. Before narrowing this set
further, prove with an end-to-end test that doing so doesn't reintroduce
that deadlock.

## Cold start: two numbers, not one

`anomaly.Score` deliberately does *not* suppress its `Score` for a
never-before-seen `Fingerprint` — a brand-new fingerprint scores near-max
novelty. Suppressing it there would destroy information a downstream
decision needs. Instead, `Confidence` is reported separately, and
`trust.Compute` is where the two are recombined:
`effectiveAnomaly = Anomaly.Score * Anomaly.Confidence`. At `Confidence
= 0`, a maximally novel event contributes zero anomaly penalty to
`TrustScore`, which falls back to identity and context alone. Do not
"fix" cold start by capping `Score` directly in `internal/anomaly` —
that collapses two different questions ("how different is this" and
"how much should we trust that reading") into one number and breaks
this mechanism.

## Some risk cannot be learned away

`anomaly.Config.SensitiveTargetFloor` sets a minimum anomaly
contribution for specific destinations (e.g. a secrets manager) that
persists regardless of how familiar the baseline becomes with that
fingerprint. `ContextRisk` in general is a config-driven, not a
learned, signal. This is what stops an attacker (or a persistently
misbehaving process) from normalizing access to something sensitive
just by repeating it — see
`TestAnalyzeSensitiveTargetFloorEndToEnd` for the proof that full
maturity does not erase the floor.

## Identity is an input, not a computation

`Actor.IdentityConfidence` is something Trustvian trusts, not
something it computes — Trustvian is not an authenticator. Every stage
that consumes it (`trust.Compute`) treats it as an opaque external
signal. If you're tempted to derive or adjust `IdentityConfidence`
from behavioral data, that crosses a boundary this design deliberately
keeps separate: behavior can reduce *trust*, but should not be used to
retroactively second-guess *identity*.

## Scope baseline/fingerprint keys defensively, even for a single-tenant OSS core

`baseline.Key{ActorID, Environment}` is a composite key from Slice 4
onward, even though the OSS engine has no multi-tenancy. This is cheap
now and expensive to retrofit later — see Architecture Risk #2 in the
project's design notes. Any new place that needs to key data by actor
should use the same composite pattern, not a bare string ID.

## Explainability is enforced, not aspirational

Every `Anomaly` retains its `Contributors` (signal name, value, weight,
detail). Every `policy.Result` carries a non-empty `Explanation`. These
are checked by tests
(`TestEvaluateAlwaysProducesNonEmptyExplanationReason`), not just
asserted in comments. A new signal, rule, or decision path that can't
explain itself this way is incomplete, not "explainability to add
later."

## No arbitrary formulas

If a constant or combination can't be justified in a code comment
(why noisy-OR and not an average; why multiplicative and not
additive; why this EWMA alpha), it doesn't belong in a scoring path.
See the rationale comments in `internal/anomaly.combine` and
`internal/trust.Compute` for the bar to match — including the explicit
note that the project spec's own worked trust-score example doesn't
reproduce under any principled formula, and was treated as illustrative
rather than reverse-engineered against.
