# Domain Model

The domain concepts behind each pipeline stage, and how they relate.
For package/API details see [ARCHITECTURE.md](ARCHITECTURE.md) and
[Go SDK Guide](sdk-guide.md); for how these concepts prevent specific
attacks, see [SECURITY.md](SECURITY.md).

```
Event → Features → Fingerprint → Baseline → Anomaly → Trust → Policy → Decision
```

## Event

The atomic unit: one observed action. Package `event`
(`github.com/Trustvian/trustvian/event`) — see
[Go SDK Guide § the Event type](sdk-guide.md#the-event-type) for the
full field reference.

- **Actor** — who/what performed the action: `ID`, `Type` (`service`,
  `user`, `service_account`, `ai_agent`, `device`, `unknown`), and
  `IdentityConfidence` — an *input* Trustvian trusts (sourced from
  upstream authentication), never something Trustvian computes.
- **Operation** — what was done: `Category` (`http`, `db`, `rpc`,
  `tool`, `external`), `Name` (e.g. `"POST /payment"`,
  `"search_customer"`), and optional `Direction` (`inbound`/`outbound`,
  from `SpanKind` when sourced via OTel).
- **Target** — the destination, when there is a distinct one (a
  service, database, or host). Optional.
- **Context** — deployment `Environment` plus, when available, OTel
  `TraceID`/`SpanID` for correlation.
- **Metadata (`Attributes`)** — an open `map[string]any`. Two keys
  carry defined meaning (`duration_ms`, `error` — see
  [Go SDK Guide](sdk-guide.md#the-event-type)); everything else passes
  through unmodified, available to a custom `ContextRisk` function.

`Event` is treated as an immutable value: nothing in the domain
mutates one after construction.

## Feature

`internal/features.Extract(Event) Features` splits an event's
dimensions into two kinds, because they play different roles
downstream:

- **Stable features** — `ActorType`, `OperationCategory`,
  `OperationName`, `TargetName`, `Environment`. These identify *what
  kind of behavior* this is and feed the `Fingerprint`.
- **Volatile features** — `Timestamp`, `Latency`, `Error`. These are
  per-event, noisy, and feed `Anomaly` directly; they never become
  part of a `Fingerprint`.

`Extract` is a pure function, deterministic and (in the common case)
zero-allocation — see [PERFORMANCE.md](PERFORMANCE.md).

## Fingerprint

`internal/fingerprint.Compute(StableFeatures) Fingerprint` derives a
deterministic identity — an `ID` (an FNV-1a hash over the five stable
dimensions) plus the `Stable` snapshot itself, retained for
explainability. Two events with identical stable dimensions always
produce the same `Fingerprint.ID`, regardless of how their volatile
data (latency, timestamp, errors) differs.

This is deliberately a *per-event-shape* identity — one ID per
distinct `(ActorType, OperationCategory, OperationName, TargetName,
Environment)` combination — not an aggregated, all-behavior profile
for an actor. The aggregation ("the set of fingerprints this actor is
known to use") emerges naturally from `Baseline`'s map, keyed by
`Fingerprint.ID`, rather than being tracked separately.

## Baseline

`internal/baseline.Baseline` is the statistical history for one
`Key{ActorID, Environment}`: a map from `Fingerprint.ID` to
`FingerprintStats`.

- **Learning** — `Baseline.Observe(fp, volatile, now) Baseline` is a
  pure, copy-on-write update: it never mutates the receiver, it
  returns a new `Baseline`. This is what makes a value read via
  `Store.Get` a permanently valid snapshot, safe to use without
  holding any lock.
- **Updating** — per-`Fingerprint` statistics use an EWMA
  (exponentially-weighted moving average) for latency mean/variance
  and error rate — not a plain cumulative (unweighted) average. This
  reconciles two needs at once: O(1) memory with no raw samples
  retained (in the spirit of Welford's online algorithm), and *decay*,
  so legitimate behavioral drift is absorbed over time rather than
  requiring a manual reset.
- **Cold start** — `FingerprintStats.Count` is the maturity counter: how
  many times this specific fingerprint has been observed.
  `internal/baseline` only counts; it does not itself decide what
  count is "mature" — that threshold
  (`anomaly.Config.MinObservations`) belongs to the consumer that
  actually needs to make that judgment call.
- **Confidence** — derived downstream (in `internal/anomaly`) as
  `Count / MinObservations`, capped at 1. `Baseline` doesn't compute a
  confidence value itself; it exposes the raw material.
- **Expiration** — not implemented as active pruning today; the EWMA
  decay means stale patterns lose statistical weight over time rather
  than being explicitly expired. See [ROADMAP.md](ROADMAP.md).

## Anomaly

`internal/anomaly.Score(Features, Fingerprint, Baseline, Config)
Anomaly` combines up to four independent signals via a **noisy-OR**
combination — `score = 1 - Π(1 - value_i · weight_i)` — chosen
specifically because a single severe signal should dominate the
result, not be diluted by averaging against several unrelated benign
signals:

| Signal | Fires when |
|---|---|
| `categorical_novelty` | The fingerprint is unfamiliar or below `MinObservations` maturity |
| `latency_deviation` | Current latency's z-score against the baseline's EWMA mean/stddev exceeds a threshold |
| `error_deviation` | An error occurred against a fingerprint whose baseline error rate is low |
| `sensitive_target` | The destination is in `Config.SensitiveTargetFloor` — a fixed penalty that persists *regardless of familiarity* |

**Score and Confidence are reported separately, on purpose.** A
brand-new fingerprint scores near-maximum novelty (`Score` near 1) but
with `Confidence` at 0 (`Count / MinObservations`). `internal/anomaly`
does not suppress `Score` for low confidence — see
[ARCHITECTURE.md § cold start](ARCHITECTURE.md#cold-start-two-numbers-not-one)
for why, and how the two are recombined in `Trust`.

Every contributing signal (`Name`, `Value`, `Weight`, `Detail`) is
retained on `Anomaly.Contributors` — this is the explanation for "which
behavioral signals contributed" and "why the score changed."

## Trust and Risk

`internal/trust.Compute(Anomaly, IdentityConfidence, ContextRisk,
Config) Trust`:

```
effectiveAnomaly = Anomaly.Score * Anomaly.Confidence
TrustScore        = IdentityConfidence * (1 - effectiveAnomaly) * (1 - ContextRisk)
```

Multiplicative, not averaged: trust is capped by its weakest factor,
the same way a chain is only as strong as its weakest link. A single
severe, *confidently measured* anomaly, or a high `ContextRisk`, can
drive `TrustScore` toward zero even when other inputs are high.

`Risk` (`RiskLevel`: `low`/`medium`/`high`/`critical`) is a
configurable-threshold bucket derived from `1 - TrustScore` — the
residual distrust. `IdentityConfidence` and `ContextRisk` are inputs
`trust.Compute` never computes itself: identity comes from upstream
authentication, context risk from caller-supplied, deterministic,
config-driven classification (not learned) — see
[SECURITY.md](SECURITY.md) for why context risk is deliberately not
purely learned.

`Trust` retains every input (`IdentityConfidence`, `AnomalyScore`,
`AnomalyConfidence`, `ContextRisk`) alongside `Score` and `Risk` — the
relationship between anomaly, risk, and trust is never collapsed into
one opaque number.

## Policy and Decision

`internal/policy.Policy.Evaluate(Input) Result` turns `Trust` plus an
event's stable features into a final `Decision`. See
[Policy Guide](policy-guide.md) for the full reference; in domain
terms:

- **Policy is data**, not code: an ordered `[]Rule` plus a mandatory
  default, not per-rule Go closures.
- **Decision** is one of `ALLOW`, `OBSERVE_ONLY`, `ALERT`,
  `CHALLENGE`, `REQUIRE_APPROVAL`, `BLOCK`.
- **Evaluation is first-match-wins**, deterministic, and fails closed:
  an unconfigured or misconfigured policy resolves to `BLOCK`, never a
  silent `ALLOW`.
- **Explanation** — every `Result` carries `RuleName` (which rule
  fired, if any), `Reason` (a human-readable explanation), and
  `MatchedDefault` (whether no rule matched and the default applied).
  This is "which policy was evaluated" and "why the final decision was
  produced," verified by a test that checks every `Decision` across
  several policies has a non-empty reason.

`Engine.Result` (root package) is the complete record tying all of
this together: the original `Event`, `Features`, `Fingerprint`,
`Anomaly` (with its contributors), `Trust` (with every input), and the
final `Decision` + `Explanation`. Nothing is discarded on the way to
the final answer — that completeness is what makes every decision
explainable end to end, not just at the policy stage.

This document describes the domain model as it exists today. Planned
extensions to it (a `Target` category dimension, a frequency-deviation
anomaly signal, AI-agent session/delegation fields, and others) are
scoped in [ROADMAP.md](ROADMAP.md) and [`tasks/`](tasks/) — each will
update this document when it actually ships, not before.
