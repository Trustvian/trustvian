# Security Model

This document describes the threats Trustvian's design accounts for,
what's actually implemented today versus deliberately deferred, and
where to find each protection in code. See
[`.claude/rules/security.md`](../.claude/rules/security.md) for the
same principles phrased as engineering rules for future changes.

Trustvian is not itself an authentication system, a database, or an
enforcement point in the network path — it is a scoring and decision
engine. Its security model is about the integrity of *its own
reasoning* (can its scores/decisions be manipulated or bypassed), not
about securing the transport or storage layers around it (those are
explicitly out of core scope — see
[`docs/ARCHITECTURE.md`](ARCHITECTURE.md)).

## Test index

Every threat below is backed by a specific, named test, not just a
design argument. This table exists so that fact is verifiable at a
glance rather than requiring a read of every section
([`docs/tasks/012-security-tests.md`](tasks/012-security-tests.md)).
Threats whose tests already existed before that task are referenced
here, not moved or rewritten.

| Threat | Test(s) |
| --- | --- |
| Identity confusion (cross-actor isolation) | `TestAnalyzeCrossActorIsolation` in [`engine_test.go`](../engine_test.go) |
| Baseline poisoning | `TestObserveLearnsOnlyFromEligibleDecisions`, `TestAnalyzeSensitiveTargetFloorEndToEnd` in [`engine_test.go`](../engine_test.go); `TestFingerprintStatsIgnoresNonPositiveInterval`, `TestFingerprintStatsOutOfOrderObservationDoesNotDistortNextInterval` in [`internal/baseline/baseline_test.go`](../internal/baseline/baseline_test.go); `TestScoreFrequencyDeviation` (negative-interval subtests) in [`internal/anomaly/anomaly_test.go`](../internal/anomaly/anomaly_test.go) |
| Malicious agents / privilege escalation | `TestAnalyzeSensitiveTargetFloorEndToEnd` in [`engine_test.go`](../engine_test.go) |
| Policy bypass | `TestEvaluateFailsClosedOnZeroValuePolicy`, `TestEvaluateFailsClosedOnInvalidDefaultAction`, `TestEvaluateFailsClosedOnEmptyDefaultReason` in [`internal/policy/policy_test.go`](../internal/policy/policy_test.go) |
| Malformed events / extreme input values | `TestValidateRejectsNonFiniteIdentityConfidence`, `TestValidateAcceptsVeryLongActorID` in [`event/event_test.go`](../event/event_test.go); `TestAnalyzeNegativeDurationDoesNotCorruptTrustScore`, `TestAnalyzeLargeAttributesMapDoesNotPanic` in [`engine_test.go`](../engine_test.go) |
| Concurrency issues | `TestInMemoryObserveConcurrentSameKey`, `TestInMemoryObserveConcurrentDistinctKeys` in [`internal/store/store_test.go`](../internal/store/store_test.go); `TestFileStoreObserveConcurrentSameKey`, `TestFileStoreObserveConcurrentDistinctKeys` in [`internal/store/file_test.go`](../internal/store/file_test.go) |
| Resource exhaustion | `TestAnalyzeLargeAttributesMapDoesNotPanic`, `TestObserveUnboundedFingerprintsDoesNotPanic` in [`engine_test.go`](../engine_test.go) |
| Explainability | `TestEvaluateAlwaysProducesNonEmptyExplanationReason` in [`internal/policy/policy_test.go`](../internal/policy/policy_test.go) |

## Threats considered

### Telemetry spoofing

**Threat:** a producer sends fabricated or misleading event data to
manipulate a decision.

**Status: partially addressed, by design boundary.** Trustvian treats
`Actor.IdentityConfidence` as an external input it trusts, never
something it computes (`internal/trust`) — it is explicitly *not* an
authenticator. If upstream telemetry is spoofed with a high
`IdentityConfidence` and no compensating signal, Trustvian will trust
it accordingly; this is a stated boundary, not a gap being hidden.
Verifying the telemetry pipeline itself (mTLS, signed spans, a trusted
collector) is the deploying application's responsibility, upstream of
this engine. **Future work:** none planned in-core; this is inherently
a transport/collection-layer concern.

### Identity confusion

**Threat:** two different actors (or the same actor in different
environments/tenants) collide on the same behavioral history, letting
one actor's baseline apply to another.

**Status: implemented, and verified end-to-end.** `baseline.Key{ActorID,
Environment}` is a composite key from the start, not a bare actor-ID
string — even though the OSS engine is single-tenant today. This is
deliberately cheap now and expensive to retrofit later (see
[ADR 0004](adr/0004-narrow-store-port-in-memory-only.md)). A future
multi-tenant `TenantID` dimension is an additive extension of the same
pattern, not a redesign. This was true by construction from
`baseline.Key`'s shape but not directly proven end-to-end through
`Engine` until `TestAnalyzeCrossActorIsolation` in
[`engine_test.go`](../engine_test.go)
([task 012](tasks/012-security-tests.md)): two actors that produce an
otherwise identical stable feature shape (same operation, same target,
same environment) never share `Baseline` state — actor-a is matured
over 30 observations, and actor-b's first-ever event for the exact same
shape still registers zero `Anomaly.Confidence`.

### Replay

**Threat:** a captured, legitimate event is resubmitted to
artificially reinforce a baseline or repeat a decision.

**Status: not implemented; explicitly deferred.** `Event.ID` and
`Timestamp` exist and could support a bounded-window dedup cache, but
this is an adapter/collection-layer concern (where events first enter
the system), not something `Engine.Analyze`/`Observe` currently do —
the core engine is stateless per call except for the `Baseline` it's
explicitly asked to update. **Future work:** dedup at the OTel adapter
or Collector processor layer.

### Baseline poisoning

**Threat:** an attacker (or a persistently misbehaving process)
gradually "trains" the baseline into treating malicious behavior as
normal by repeating it.

**Status: implemented, and verified end-to-end.** `Engine.Observe`
only learns from `Decision`s where the action *proceeded*
(`ALLOW`/`OBSERVE_ONLY`/`ALERT`); anything held or stopped
(`CHALLENGE`/`REQUIRE_APPROVAL`/`BLOCK`) is never folded into the
baseline. This gating lives inside `Observe` itself, not in caller
discipline — it's safe to call unconditionally after every `Analyze`.
See `TestObserveLearnsOnlyFromEligibleDecisions` and
`TestAnalyzeSensitiveTargetFloorEndToEnd` in
[`engine_test.go`](../engine_test.go), which specifically proves a
`BLOCK`ed, sensitive-destination pattern cannot mature its way to
trust no matter how many times it's repeated.

A related, non-obvious nuance: the eligible-decision set includes
`ALERT`, not only `ALLOW`/`OBSERVE_ONLY`. Excluding `ALERT` created a
real deadlock during development — a brand-new, entirely benign
fingerprint can transiently cross into `ALERT`-level risk purely from
partial maturity, and if that state were ineligible for learning, it
could never mature past it. See `eligibleForLearning`'s doc comment in
[`engine.go`](../engine.go).

**A second, structurally different poisoning path: skewing an EWMA
with a single allowed-but-extreme input.** The gating above answers
"can repeating *blocked* behavior wear the system down?" — no. It does
not, by itself, answer "can one *permitted* observation move a learned
statistic further than it should?" `FingerprintStats` keeps three
exponentially-weighted moving averages (latency, inter-observation
interval, error rate) with `emaAlpha = 0.2`, so a single sample moves
the mean by 20% of its distance and decays only gradually. A learning-
eligible outlier therefore has real, if bounded and self-correcting,
influence — that is the intended cost of EWMA decay (absorbing
legitimate drift without a manual reset), not a defect. Two things
follow:

- **The interval EWMA's unbounded variant is closed.** Before the v0.1
  final-review pass, an event whose `Timestamp` preceded the
  fingerprint's `LastObserved` — clock skew, out-of-order delivery, or
  a deliberately backdated event from an untrusted producer — folded a
  *negative* interval into `IntervalMean`/`IntervalVariance`. That is
  not a bounded outlier: one backdated event can drive `IntervalMean`
  arbitrarily far negative (a 30-day backdate measured
  `IntervalMean = -143h59m52s`), which makes every subsequent on-time
  event look anomalous; and one legitimate long gap inflates
  `IntervalVariance` enough to desensitize the frequency signal to a
  genuine burst that follows. Such an event is normally decided
  `observe_only`, i.e. fully learning-eligible, so decision gating never
  saw it. `FingerprintStats.observe` now skips interval tracking
  entirely for any non-positive interval and advances `LastObserved`
  monotonically, and `anomaly.Score` refuses to fire
  `frequency_deviation` on a negative interval as the read-side half of
  the same guard. Proven by
  `TestFingerprintStatsIgnoresNonPositiveInterval` and
  `TestFingerprintStatsOutOfOrderObservationDoesNotDistortNextInterval`
  in [`internal/baseline/baseline_test.go`](../internal/baseline/baseline_test.go),
  and by `TestScoreFrequencyDeviation`'s
  `negative interval does not fire` subtests in
  [`internal/anomaly/anomaly_test.go`](../internal/anomaly/anomaly_test.go).
- **The bounded variant remains, by design.** An extreme but
  *forward-in-time* latency, interval, or error observation that a
  policy permits still moves its EWMA, in both the latency and interval
  statistics. No per-sample clamp or outlier rejection is implemented,
  and adding one would trade away the decay property the EWMA exists
  for. The mitigating factors are that the influence is bounded by
  `emaAlpha` and decays as normal traffic resumes, that
  `Anomaly.Confidence` is reported separately so a thin baseline is
  never mistaken for a confident one, and that
  `SensitiveTargetFloor`-gated risk cannot be learned away at all.
  **Future work:** if a deployment needs it, per-sample outlier
  rejection belongs in `FingerprintStats.observe` alongside the
  ordering guard, not in caller discipline.

**Persistence-adjacent note:** `store.FileStore`
([ADR 0006](adr/0006-file-backed-persistent-store.md)) flushes to disk
synchronously after every `Observe`, so an unclean shutdown loses at
most the single in-flight observation, never a corrupted or
partially-written file (writes are atomic via temp-file-plus-rename).
This does not introduce a new poisoning vector: the gating above
applies identically regardless of which `Store` implementation is
configured — `FileStore` persists exactly what `Observe` already
decided was eligible to learn, nothing more. Restarting a process using
`FileStore` resumes from the same (gated) baseline it had before the
restart, rather than the empty one `InMemory` would present — this is
the intended fix for the "every restart quietly forgets an attacker's
prior flagged behavior" gap an in-memory-only store would otherwise
leave.

### Malicious agents / privilege escalation

**Threat:** an AI agent or service account uses legitimate credentials
to access something outside its normal scope (secrets, admin
interfaces), and — because access to a sensitive destination is itself
learnable behavior — gradually normalizes that access.

**Status: implemented.** `anomaly.Config.SensitiveTargetFloor` sets a
fixed minimum anomaly contribution for specific destinations that
persists *regardless of baseline familiarity* — full maturity does not
erase it (see [DOMAIN.md § Anomaly](DOMAIN.md#anomaly) and the same
`TestAnalyzeSensitiveTargetFloorEndToEnd` test above). This is what
directly prevents the "train the baseline into trusting me" attack
path for destinations an operator has explicitly flagged as sensitive.
**Caveat:** this requires the operator to configure
`SensitiveTargetFloor` for the destinations that matter — it is not
automatic classification. See [ROADMAP.md](ROADMAP.md) for automatic
sensitivity detection as a possible future direction.

### Policy bypass

**Threat:** a missing, empty, or misconfigured policy silently
resolves to `ALLOW`, defeating enforcement without anyone noticing.

**Status: implemented.** `policy.Policy.Evaluate` fails closed to
`BLOCK` — not `ALLOW` — when `DefaultAction` is unset or invalid, or
when `DefaultAction` is set but `DefaultReason` is empty. There is no
code path from "the config is wrong" to a silent allow; this is
verified directly by
`TestEvaluateFailsClosedOnZeroValuePolicy`,
`TestEvaluateFailsClosedOnInvalidDefaultAction`, and
`TestEvaluateFailsClosedOnEmptyDefaultReason` in
[`internal/policy/policy_test.go`](../internal/policy/policy_test.go).

### Malformed events / extreme input values

**Threat:** a producer sends a structurally valid but adversarial
`Event` — `NaN`/`Inf` `IdentityConfidence`, empty or extremely long
identifiers, negative durations, deeply nested or very large
`Attributes` — attempting to crash the engine, corrupt a score, or
smuggle bad data past validation.

**Status: implemented, with two deliberate exceptions documented as
accepted behavior, not gaps.**

- `Event.Validate()` rejects empty required fields, out-of-range
  `IdentityConfidence` (including `NaN`, `+Inf`, `-Inf`), and invalid
  enum values. The `NaN` case was a genuine, empirically-confirmed gap
  closed by this task: Go's `NaN < 0`/`NaN > 1` are both always false,
  so the pre-existing range check silently passed a `NaN`
  `IdentityConfidence` through `Actor.validate()` (`event/event.go`)
  until an explicit `math.IsNaN` check was added. `+Inf`/`-Inf` were
  already correctly rejected by the range check before this task; see
  `TestValidateRejectsNonFiniteIdentityConfidence` in
  [`event/event_test.go`](../event/event_test.go), which proves all
  three sub-cases individually rather than assuming they behaved alike.
- `Actor.ID` has no length limit, and `TestValidateAcceptsVeryLongActorID`
  in [`event/event_test.go`](../event/event_test.go) asserts that a
  100,000-character `Actor.ID` is accepted, not rejected — this is
  deliberate current behavior per [task 012](tasks/012-security-tests.md)'s
  Non-Goals (no length limit added absent a concrete DoS vector), stated
  explicitly rather than left an untested assumption.
- A negative `duration_ms` attribute is not rejected by `Validate` (only
  `IdentityConfidence` and enum fields are checked there) and flows
  through `features.Extract` into `internal/anomaly`'s latency z-score
  math as a negative `time.Duration`. This was traced through
  empirically, not assumed: `(currentNS - mean) / stddev` is a
  well-defined finite division whenever the baseline's `stddev != 0`,
  regardless of the sign of `currentNS`, and `min(z/threshold, 1)` then
  clamps it into the signal's normal `[0,1]` range exactly like any
  other extreme deviation — no `NaN`/`Inf` reaches `Anomaly.Score` or
  `Trust.Score`. `TestAnalyzeNegativeDurationDoesNotCorruptTrustScore`
  in [`engine_test.go`](../engine_test.go) pins this down as a
  regression rather than an implicit assumption; no code change was
  needed here because none was demonstrated necessary.
- `trust.Compute` separately, defensively clamps its numeric inputs to
  `[0,1]` regardless of what's passed (`internal/trust/trust.go`), as a
  second line of defense independent of the above.
- A large `Attributes` map (100,000 keys) does not panic or error
  `Engine.Analyze` — see `TestAnalyzeLargeAttributesMapDoesNotPanic` in
  [`engine_test.go`](../engine_test.go), also listed under "Resource
  exhaustion" below.

### Concurrency issues

**Threat:** concurrent `Get`/`Observe` calls — across the same actor's
key or across many distinct actors' keys — race with each other and
corrupt shared `Baseline` state (a lost update, a torn read, or a data
race that only a `-race` build would catch).

**Status: implemented, and verified under `-race`.** The mechanism this
is safe by construction, not by luck, is `internal/baseline`'s
immutability: `Baseline.Observe` never mutates its receiver — it always
returns a brand-new `Baseline` value with its own `Fingerprints` map
(`internal/baseline/baseline.go`). That means a `Baseline` value a
caller already holds (e.g. from an earlier `Store.Get`) is a permanently
valid snapshot; nothing can retroactively change it out from under a
reader. `internal/store`'s sharded-lock design is the concurrency-safety
layer built on top of that immutability: it serializes the
read-modify-write around each key's `Observe` (so concurrent writers to
the *same* key don't lose an update) while letting writes to *distinct*
keys proceed independently (no unnecessary cross-actor lock contention).
This is verified directly, under `go test -race`, by:

- `TestInMemoryObserveConcurrentSameKey` and
  `TestFileStoreObserveConcurrentSameKey`
  ([`internal/store/store_test.go`](../internal/store/store_test.go),
  [`internal/store/file_test.go`](../internal/store/file_test.go)) —
  many goroutines call `Observe` concurrently against the *same* key and
  assert the final `Count` equals exactly the number of calls made, with
  no lost update.
- `TestInMemoryObserveConcurrentDistinctKeys` and
  `TestFileStoreObserveConcurrentDistinctKeys` (same files) — many
  goroutines call `Observe` concurrently, each against its *own* key,
  and assert every key ends up with its own independent, correct
  `Count`, proving concurrent writes to different actors never
  interfere with each other.

### Resource exhaustion

**Threat:** an attacker (or a misbehaving legitimate producer) sends
input designed to consume disproportionate CPU or memory relative to
its size — an unbounded `Attributes` map, or an actor generating an
unbounded number of distinct fingerprints to grow `Baseline` without
limit.

**Status: safety property tested (no panic, no error); per-call cost
characterized and flat; total heap footprint still unbounded by
design.** There is no per-event size limit on `Attributes` today, and
`store.InMemory` has no eviction policy.
`TestAnalyzeLargeAttributesMapDoesNotPanic` in
[`engine_test.go`](../engine_test.go) proves a 100,000-key `Attributes`
map does not panic or error `Engine.Analyze` (only `duration_ms`/`error`
are ever read out of it, so per-event cost is proportional to what's
consumed, not to the map's total size).
`TestObserveUnboundedFingerprintsDoesNotPanic` in
[`engine_test.go`](../engine_test.go) proves a single actor producing
5,000 distinct fingerprints does not panic or error `Engine.Observe`.
Neither test bounds memory growth itself — that's a deliberate scope
line from [task 012](tasks/012-security-tests.md): the *safety*
property (no panic, no deadlock) is this task's concern, the *growth
curve* was [task 011](tasks/011-performance.md)'s.

[Task 011](tasks/011-performance.md) has since run that
characterization. `BenchmarkInMemoryMemoryGrowth` measures
`store.InMemory.Observe` against stores pre-populated with 100, 1,000,
and 10,000 distinct keys, and the *per-call* cost is flat: `B/op` and
`allocs/op` are exactly identical (464 B, 3 allocs) at every key count,
with only a ~15% `ns/op` drift attributable to map cache locality. So a
store already holding 10,000 actors is not more expensive per event
than one holding 100 — an attacker cannot degrade per-event throughput
by inflating the key space. See
[PERFORMANCE.md § measured results](PERFORMANCE.md#measured-results).

What that measurement does *not* answer, and what remains genuinely
open, is the **total** heap footprint: per-call cost being flat says
nothing about the aggregate size of a `Baseline` map that only ever
grows, since neither `InMemory` nor `FileStore` evicts anything. An
actor generating unbounded distinct fingerprints still grows resident
memory without limit; measuring that would need `runtime.MemStats`
sampled across a long run rather than `go test -bench`, and is named as
still-open in
[PERFORMANCE.md § what's not benchmarked (yet)](PERFORMANCE.md#whats-not-benchmarked-yet).
**Future mitigation:** an enforced limit
(per-event `Attributes` size, `Baseline` eviction, or a
`FingerprintStats.IsStale`-driven pruning pass — the staleness signal
task 003 shipped is the natural input to one) remains a decision to
make when a concrete deployment shows it's needed, not preemptively.
The distinction matters for prioritization: this is a capacity-planning
question, not a per-request denial-of-service one.

### Future multi-tenant isolation

**Threat:** in a multi-tenant deployment, one tenant's behavioral data
or policy decisions leak into or influence another's.

**Status: not implemented; the data model is prepared for it.**
Trustvian's OSS core is explicitly single-tenant (multi-tenancy, RBAC,
and centralized management are Trustvian Control/Cloud concerns — see
[ARCHITECTURE.md § relationship to Control/Cloud](ARCHITECTURE.md#relationship-to-trustvian-controlcloud)).
However, `baseline.Key`'s composite `(ActorID, Environment)` shape
means the data is already scoped in a way a future `TenantID` addition
extends rather than restructures — a deliberate choice to make that
future work an access-control addition, not a data migration.

## Explainability as a security property

Every `Anomaly` retains its `Contributors`; every `policy.Result`
carries a non-empty `Explanation`. This isn't incidental — an
un-explainable decision is itself a kind of risk (an operator can't
audit or contest what they can't see the reasoning for). These
properties are checked by tests
(`TestEvaluateAlwaysProducesNonEmptyExplanationReason`), not just
documented as an aspiration.

## What Trustvian does not protect against

- Compromise of the process it runs in (memory tampering, a malicious
  binary). Out of scope for an in-process library.
- Weaknesses in the identity/authentication system upstream of it — it
  consumes `IdentityConfidence` as an input, it does not produce it.
- Network-level attacks against however events are transported to it —
  that's the transport/collector's responsibility.
