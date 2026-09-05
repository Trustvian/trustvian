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
  service, database, or host). Optional. `Category` (`internal`,
  `external`, `database`) optionally classifies what *kind* of
  destination it is; like `Direction`, the zero value means
  unclassified and is never checked by `Validate()`.
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
  `OperationName`, `TargetName`, `TargetCategory`, `Environment`.
  These identify *what kind of behavior* this is and feed the
  `Fingerprint`. `TargetCategory` (added in
  [task 001](tasks/001-feature-model.md)) mirrors `Event.Target.Category`
  and is optional — it flows through `Extract` and, as of
  [task 002](tasks/002-fingerprint.md), is part of
  `fingerprint.Compute`'s hash.
- **Volatile features** — `Timestamp`, `Latency`, `Error`. These are
  per-event, noisy, and feed `Anomaly` directly; they never become
  part of a `Fingerprint`.

`Extract` is a pure function, deterministic and (in the common case)
zero-allocation — see [PERFORMANCE.md](PERFORMANCE.md).

## Fingerprint

`internal/fingerprint.Compute(StableFeatures) Fingerprint` derives a
deterministic identity — an `ID` (an FNV-1a hash over the stable
dimensions) plus the `Stable` snapshot itself, retained for
explainability. Two events with identical stable dimensions always
produce the same `Fingerprint.ID`, regardless of how their volatile
data (latency, timestamp, errors) differs.

This is deliberately a *per-event-shape* identity — one ID per
distinct `(ActorType, OperationCategory, OperationName, TargetName,
TargetCategory, Environment)` combination — not an aggregated,
all-behavior profile for an actor. The aggregation ("the set of
fingerprints this actor is known to use") emerges naturally from
`Baseline`'s map, keyed by `Fingerprint.ID`, rather than being tracked
separately.

**What feeds the hash, in order.** `Compute` writes, in this fixed
order: the version marker (see below), then `ActorType`,
`OperationCategory`, `OperationName`, `TargetName`, `TargetCategory`,
`Environment` — the same six fields `internal/features.Extract`
classifies as *stable* (see [§ Feature](#feature)). No volatile field
(`Timestamp`, `Latency`, `Error`) and no event-instance identifier
(`Event.ID`, `TraceID`, `SpanID`) ever enters the hash — that's what
makes the ID a behavioral-shape key rather than a per-request one.

**Why FNV-1a.** `hash/fnv`'s 64-bit variant is fast (no cryptographic
primitives), non-cryptographic, and gives a large enough ID space
(2^64) that accidental collisions across genuinely distinct behavioral
shapes are not a practical concern. This is a content-addressed
identity key for a map lookup, not a security boundary — nothing
downstream trusts `Fingerprint.ID` to resist a deliberate,
computationally-motivated collision attack, so a slower cryptographic
hash would only add cost without buying a real property this design
needs.

**Field-boundary collision protection.** Each field is written through
`writeField`, which appends a NUL (`\x00`) byte after the field's
bytes. Without this, two structurally different inputs could hash
identically by having bytes shift across an (unmarked) field boundary
— e.g. `OperationName="ab", TargetName="c"` and `OperationName="a",
TargetName="bc"` would otherwise concatenate to the same byte stream.
`TestComputeAvoidsFieldBoundaryCollision` in
`internal/fingerprint/fingerprint_test.go` pins this property.

**Versioning.** `Compute` writes a `fingerprintVersion` constant
(currently `"1"`) as the *first* field, ahead of every stable
dimension, using the same `writeField` NUL-separator convention. This
exists so that a future change to which dimensions feed the hash, or
to the hash algorithm itself, produces a *disjoint* ID space from
today's rather than silently reinterpreting existing IDs under new
semantics — an in-memory-only `Store` would survive that silently
(restart clears everything), but a persistent `Store` would not: a
stored baseline computed under one fingerprint composition could be
misread after an upgrade changes what its ID means. Bump
`fingerprintVersion` whenever the stable field set or hash algorithm
changes — version `"1"` is the version under which `TargetCategory`
first became part of the hash (see
[task 002](tasks/002-fingerprint.md)). There is deliberately no
separate `Version` field on `Fingerprint`: no current consumer needs to
read the version independent of the ID it's baked into, so exposing
one would be exactly the kind of interface `.claude/rules/architecture.md`
says to add only when needed, not speculatively.

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
  (exponentially-weighted moving average) for latency mean/variance,
  inter-observation interval mean/variance, and error rate — not a
  plain cumulative (unweighted) average. This reconciles two needs at
  once: O(1) memory with no raw samples retained (in the spirit of
  Welford's online algorithm), and *decay*, so legitimate behavioral
  drift is absorbed over time rather than requiring a manual reset.
  The interval EWMA (`IntervalObservations`/`IntervalMean`/
  `IntervalVariance`) is computed from the *previous* `LastObserved`
  before it's overwritten, so it captures how much time elapsed since
  the fingerprint's last occurrence — the raw material
  `internal/anomaly`'s `frequency_deviation` signal (task
  [004](tasks/004-anomaly.md)) scores against. It has no interval to
  record on a fingerprint's first observation (`IntervalObservations`
  stays 0), mirroring `LatencyObservations`' cold-start behavior.
- **Ordering** — an observation whose timestamp does not strictly
  follow `LastObserved` (clock skew, out-of-order delivery, a
  deliberately backdated event) is still counted, but its interval is
  *not* folded into the EWMA: a negative interval is an absence of
  timing information, not a measurement. `LastObserved` correspondingly
  advances monotonically and never regresses, so the next in-order
  event still measures from the freshest observation and `IsStale` is
  never fooled into reporting a fingerprint as staler than the newest
  evidence held for it. Without this guard a single backdated event
  drags `IntervalMean` below zero and makes every subsequent on-time
  event look anomalous — see [SECURITY.md § baseline poisoning](SECURITY.md).
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
Anomaly` combines up to five independent signals via a **noisy-OR**
combination — `score = 1 - Π(1 - value_i · weight_i)` — chosen
specifically because a single severe signal should dominate the
result, not be diluted by averaging against several unrelated benign
signals:

| Signal | Fires when |
|---|---|
| `categorical_novelty` | The fingerprint is unfamiliar or below `MinObservations` maturity |
| `latency_deviation` | Current latency's z-score against the baseline's EWMA mean/stddev exceeds a threshold |
| `frequency_deviation` | The current inter-observation interval's z-score against the baseline's EWMA mean/stddev interval exceeds `Config.FrequencyZThreshold` — the "this actor normally calls this operation every 10s; it just called it every 100ms" signal (a classic abuse/exfiltration pattern). Requires the fingerprint to be known, at least one recorded interval (`FingerprintStats.IntervalObservations > 0`), and a strictly positive current interval; a fingerprint's very first observation has no prior `LastObserved` to measure from, so it never fires on cold start, mirroring `latency_deviation`'s `LatencyObservations > 0` gate. **Contributes 0 to `Score` by default** — see below |
| `error_deviation` | An error occurred against a fingerprint whose baseline error rate is low |
| `sensitive_target` | The destination is in `Config.SensitiveTargetFloor` — a fixed penalty that persists *regardless of familiarity* |

**Two of the five ship inert.** `DefaultConfig()` leaves
`SensitiveTargetFloor` empty (so `sensitive_target` never fires until an
operator names their sensitive destinations) and sets `FrequencyWeight`
to `0` (so `frequency_deviation` is detected and reported in
`Contributors`, but multiplies to nothing inside the noisy-OR). In both
cases the mechanism is complete and tested; only the deployment-specific
value that makes it count is left to the operator, because no default is
correct everywhere.

For `frequency_deviation` specifically, the reason is calibration
against real jitter. The signal divides by the standard deviation of a
fingerprint's own inter-event intervals, so on traffic with only
milliseconds of natural jitter around a ten-second cadence, an event a
few milliseconds off the mean already exceeds `FrequencyZThreshold` and
clamps the signal to `1.0`. At the `0.6` weight originally shipped, that
alone carried a fully-familiar actor to `RiskHigh` on routine traffic
(`TestAnalyzeOrdinaryCadenceJitterDoesNotElevateRisk` pins this). Measure
your own fleet's `IntervalMean` and the stddev implied by
`IntervalVariance` before raising the weight.

**Two branches, not one.** `frequencySignal` (and `latencySignal`
identically) switches on whether the baseline's standard deviation is
usable. Above the threshold — 1ms for intervals, 1µs for latency — it
takes the z-score path: `value = min(|z| / ZThreshold, 1)`. At or below
it, dividing by a near-zero stddev would produce an unbounded or `NaN`
z-score, so it degrades to an exact-match test instead: any interval
different from the mean at all scores `1.0`, anything identical scores
`0`. That branch is correct for genuinely fixed-cadence traffic — a cron
job firing at exactly `00:00:00` every hour, a fixed-interval poller —
where "different from the mean" really is the whole signal. It is also
why perfectly synthetic test fixtures never exercise the z-score path:
a hand-built baseline with an exactly constant interval always lands in
this branch (see `baselineWithStableInterval` vs
`baselineWithJitteredInterval` in `internal/anomaly`'s tests).

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
one opaque number. `Trust.Explain() string` renders those retained
fields as one human-readable sentence (e.g. `"trust 0.35 (high):
identity confidence 0.97, anomaly 0.91 at full confidence, context
risk 0.10"`) — pure formatting over existing fields, no new
computation.

`TestComputeScenarioMatrixBoundsAndMonotonicity`
(`internal/trust/trust_test.go`, task
[005](tasks/005-trust-risk.md)) sweeps `IdentityConfidence`,
`Anomaly.Score`, `Anomaly.Confidence`, and `ContextRisk` each across
`{0, 0.25, 0.5, 0.75, 1}` — the full cross product — and asserts two
guarantees that were previously only implied by the formula, not
explicitly tested across the whole input space: `TrustScore` never
leaves `[0,1]`, and it is monotonic in each risk-bearing input
(increasing `Anomaly.Score` or `ContextRisk` never *increases*
`TrustScore`; increasing `IdentityConfidence` never *decreases* it).

## Policy and Decision

`internal/policy.Policy.Evaluate(Input) Result` turns `Trust` plus an
event's stable features into a final `Decision`. See
[Policy Guide](policy-guide.md) for the full reference; in domain
terms:

- **Policy is data**, not code: an ordered `[]Rule` plus a mandatory
  default, not per-rule Go closures.
- **Input** carries what a `Condition` can match against:
  `features.StableFeatures`, `trust.Trust`, and — since task 006 —
  `Attributes map[string]any`, the raw `Event.Attributes` passed
  through unchanged by `Engine.Analyze`. This is what lets
  `Condition.Attributes` match a specific key/value pair (e.g.
  `tool.category: secrets`) without inventing a general policy
  language; see [Policy Guide § matching
  Event.Attributes](policy-guide.md#example-matching-eventattributes).
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
explainable end to end, not just at the policy stage. `Result.Explain()`
renders that whole record as one human-readable summary, so answering
"why did Trustvian allow/block/challenge this" never requires
hand-assembling the story from five separate fields.

This document describes the domain model as it exists today. Planned
extensions to it (a frequency-deviation anomaly signal, AI-agent
session/delegation fields, and others) are scoped in
[ROADMAP.md](ROADMAP.md) and [`tasks/`](tasks/) — each will
update this document when it actually ships, not before.
