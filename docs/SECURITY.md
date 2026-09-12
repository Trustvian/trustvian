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
| Alert/notification delivery integrity | `TestSendSignsPayloadCorrectly`, `TestSendTamperedPayloadFailsVerification`, `TestSendDoesNotLeakSecret`, `TestNewWebhookSinkRejectsNonHTTPS`, `TestNewWebhookSinkRejectsLoopbackDestination`, `TestSendRespectsTimeout`, `TestSendPayloadTooLargeMakesNoNetworkCall` in [`alert/webhook_test.go`](../alert/webhook_test.go) |
| Configuration-input validation | `TestValidateRejectsUnsupportedVersion`, `TestValidateRejectsInvalidDefaultDecision`, `TestValidateRejectsInvalidRuleDecision`, `TestValidateRejectsInvalidActorType`, `TestValidateRejectsInvalidOperationCategory`, `TestValidateRejectsInvalidRiskLevel`, `TestValidateRejectsDuplicateRuleName`, `TestValidateRejectsEmptyRuleName`, `TestValidateRejectsTooManyRules`, `TestValidateRejectsOverlongName` in [`config/validate_test.go`](../config/validate_test.go); `TestLoadRejectsUnknownTopLevelField`, `TestLoadRejectsUnknownNestedField`, `TestLoadRejectsDuplicateYAMLKeys`, `TestLoadFileRejectsOversizedFile`, `TestLoadRejectsEmptyInput`, `TestLoadDoesNotPanicOnArbitraryInput`, `FuzzLoad` in [`config/load_test.go`](../config/load_test.go)/[`config/fuzz_test.go`](../config/fuzz_test.go) |
| Alert configuration-input validation | `TestValidateAlertConfigRejectsUnsupportedVersion`, `TestValidateAlertConfigRejectsInvalidSeverity`, `TestValidateAlertConfigRejectsInvalidDecision`, `TestValidateAlertConfigRejectsInvalidRiskLevel`, `TestValidateAlertConfigRejectsInvalidActorType`, `TestValidateAlertConfigRejectsInvalidTargetCategory`, `TestValidateAlertConfigRejectsInvalidMinAnomalyScore`, `TestValidateAlertConfigRejectsInvalidMaxTrustScore`, `TestValidateAlertConfigRejectsDuplicateRuleName`, `TestValidateAlertConfigRejectsEmptyRuleName`, `TestValidateAlertConfigRejectsTooManyRules` in [`config/alert_test.go`](../config/alert_test.go); `TestLoadAlertsRejectsUnknownField`, `TestLoadAlertsRejectsDuplicateYAMLKeys`, `TestLoadAlertsFileRejectsOversizedFile`, `TestLoadAlertsRejectsEmptyInput`, `FuzzLoadAlerts` in [`config/alert_load_test.go`](../config/alert_load_test.go) |
| Sequence state (memory bounds, ordering, cross-actor isolation) | `TestBaselineObservePredecessorCountsIsBounded`, `TestBaselineObserveOutOfOrderEventDoesNotRecordOrCorruptTransition`, `TestBaselineObservePredecessorCountsIsImmutable` in [`internal/baseline/baseline_test.go`](../internal/baseline/baseline_test.go); `TestInMemoryObserveConcurrentTransitionTracking` in [`internal/store/store_test.go`](../internal/store/store_test.go); `TestDefaultConfigTransitionWeightIsOptIn`, `TestScoreTransitionDeviation` in [`internal/anomaly/anomaly_test.go`](../internal/anomaly/anomaly_test.go); `TestAnalyzeTransitionDeviationEndToEnd` in [`engine_test.go`](../engine_test.go) |
| Transition rarity — cold start, counter overflow, poisoning, actor isolation (`v0.6` task 026) | `TestBaselineObserveManyDistinctTransitionsStayBounded`, `TestBaselineObserveOutgoingTransitionTotalIsImmutable` in [`internal/baseline/baseline_test.go`](../internal/baseline/baseline_test.go); `TestScoreTransitionRarityColdStart`, `TestScoreTransitionRarityNeverExceedsBounds`, `TestDefaultConfigTransitionRarityWeightIsOptIn` in [`internal/anomaly/anomaly_test.go`](../internal/anomaly/anomaly_test.go); `TestAnalyzeTransitionRarityCrossActorIsolation`, `TestAnalyzeTransitionRarityScoresBeforeLearning`, `TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions` in [`engine_test.go`](../engine_test.go) |
| Bounded 3-gram detection — independent cardinality bounds, counter overflow, poisoning, actor isolation (`v0.6` task 027) | `TestBaselineObserveTrigramCountsIsBounded`, `TestBaselineObserveTrigramContinuationTotalIsBounded`, `TestInMemoryObserveConcurrentTrigramTracking` (`internal/store/store_test.go`), `TestFileStoreSurvivesRestartWithTrigramState` (`internal/store/file_test.go`) in [`internal/baseline/baseline_test.go`](../internal/baseline/baseline_test.go); `TestScoreNGramRarityColdStart`, `TestScoreNGramRarityNeverExceedsBounds`, `TestDefaultConfigNGramWeightIsOptIn` in [`internal/anomaly/anomaly_test.go`](../internal/anomaly/anomaly_test.go); `TestAnalyzeNGramCrossActorIsolation`, `TestAnalyzeNGramScoresBeforeLearning`, `TestObserveNGramLearnsOnlyFromEligibleDecisions` in [`engine_test.go`](../engine_test.go) |
| Markov surprisal — zero-probability safety, correlated-signal double-counting, poisoning, actor isolation (`v0.6` task 028) | `TestScoreMarkovSurprisalUnseenTransitionNeverFires`, `TestScoreMarkovSurprisalNeverExceedsBounds`, `TestScoreMarkovSurprisalColdStart`, `TestMarkovSurprisalIsMonotonicReparameterizationOfRarity`, `TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring`, `TestScoreCombinedMarkovAndNGramSignalsRemainBounded` in [`internal/anomaly/anomaly_test.go`](../internal/anomaly/anomaly_test.go); `TestAnalyzeMarkovCrossActorIsolation`, `TestAnalyzeMarkovScoresBeforeLearning`, `TestObserveMarkovLearnsOnlyFromEligibleDecisions` in [`engine_test.go`](../engine_test.go) |
| AI Agent behavioral context — session-ID cardinality, fingerprint independence, actor isolation, delegation (`v0.7` task 014) | `TestAnalyzeAgentSessionIDDoesNotExplodeBaseline`, `TestAnalyzeAgentToolNoveltyDetectedByExistingEngine`, `TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine`, `TestAnalyzeAgentCrossActorIsolation`, `TestAnalyzeAgentDelegationContextScoredIdentically` in [`engine_test.go`](../engine_test.go); `TestFingerprintIDIndependentOfAgentContext` in [`internal/fingerprint/fingerprint_test.go`](../internal/fingerprint/fingerprint_test.go); `TestEventValidateIgnoresAgentContextFields` in [`event/event_test.go`](../event/event_test.go) |
| Approval-aware policy — policy authority over the requirement, fail-closed missing evidence, backward compatibility, behavioral independence, non-agent genericity (`v0.7` task 030) | `TestEvaluateApprovalRequiredExampleMatrix`, `TestEvaluateApprovalPolicyAuthorityEventCannotOverridePolicy`, `TestEvaluateApprovalFailSafeOnMissingEvidence`, `TestEvaluateNoApprovalRuleConfiguredIsUnaffectedByApprovalStatus` in [`internal/policy/policy_test.go`](../internal/policy/policy_test.go); `TestAnalyzeAgentApprovalPolicyAllowsApprovedDeniesUnapproved`, `TestAnalyzeApprovalPolicyBehavioralScoreIndependence`, `TestAnalyzeApprovalPolicyGenericNotHardCodedToAIAgent` in [`engine_test.go`](../engine_test.go); `TestValidateRejectsUnknownApprovalStatusDoesNotSilentlyMapToApproved` in [`config/validate_test.go`](../config/validate_test.go) |

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
- **`HourActivity` (task [017](tasks/017-baseline-time-patterns.md))
  is the same bounded, self-correcting shape, not a new threat class.**
  `FingerprintStats.HourActivity` is an EWMA-of-indicator per UTC
  hour-of-day, structurally identical to `ErrorRate` above except
  applied to 24 buckets instead of one. A single allowed-but-unusual-
  hour observation nudges its bucket the same bounded, decaying amount
  the interval/latency/error EWMAs already tolerate — no new poisoning
  primitive is introduced. It ships with `anomaly.Config.TimePatternWeight
  = 0` (see [DOMAIN.md § Anomaly](DOMAIN.md)), so this signal
  contributes nothing to `Anomaly.Score` in the default configuration
  regardless.

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

### Sequence state

**Threat:** `v0.6`'s [transition-deviation
signal](tasks/025-sequence-analysis-foundation.md) introduces the
first runtime state whose size is driven not just by an actor's
*current* fingerprint but by pairs of them — a new resource-exhaustion
surface, and a new place cross-actor or ordering mistakes could leak
information between unrelated identities.

**Status: implemented, with specific, tested mitigations** — see [ADR
0010](adr/0010-bounded-process-local-sequence-state.md) for the design
these follow from:

- **Memory exhaustion.** `FingerprintStats.PredecessorCounts` is capped
  at 64 distinct predecessor entries per destination fingerprint
  (`maxPredecessors`) — an attacker who varies the *previous* action on
  every call (trivial, since `Fingerprint.ID` derives from
  caller-controlled `Event` fields) cannot grow one entry's map without
  bound. Once at the bound, a genuinely new predecessor is not added;
  tracked entries keep accumulating normally — proven under real
  concurrent contention by
  `TestInMemoryObserveConcurrentTransitionTracking` in
  [`internal/store/store_test.go`](../internal/store/store_test.go),
  and directly by `TestBaselineObservePredecessorCountsIsBounded` in
  [`internal/baseline/baseline_test.go`](../internal/baseline/baseline_test.go).
  `Baseline.Fingerprints` itself remains unbounded, as it already was
  before this task (see [Resource exhaustion](#resource-exhaustion)
  below) — this task does not newly introduce that characteristic, only
  bounds the one genuinely new structure it does add.
- **High-cardinality identities.** Unaffected beyond the existing
  `Baseline.Fingerprints` characteristic: sequence state adds two
  scalar fields (`LastFingerprintID`, `LastFingerprintTime`) per
  `baseline.Key`, not per identity value observed — no new
  cardinality-sensitive structure.
- **Cross-actor contamination.** Structurally impossible by
  construction, not merely policy: every new field lives inside
  `Baseline`, which is already scoped to exactly one
  `baseline.Key{ActorID, Environment}` and stored behind
  `internal/store`'s existing per-`Key` sharded lock. There is no
  code path where one actor's `LastFingerprintID` or
  `PredecessorCounts` could be read or written while processing a
  different actor's event.
- **Out-of-order events.** `Baseline.Observe` only records a transition
  — and only advances `LastFingerprintID`/`Time` — when the incoming
  timestamp strictly follows the previous one, the identical guard
  `FingerprintStats.observe`'s interval statistics already use for the
  identical reason (see Baseline poisoning above): a backdated or
  replayed event must not (a) be scored as following a predecessor it
  didn't actually follow in real time, or (b) silently rewrite what the
  *next* legitimate event's transition is measured against. Proven by
  `TestBaselineObserveOutOfOrderEventDoesNotRecordOrCorruptTransition`
  and the `out-of-order event does not fire` case in
  `TestScoreTransitionDeviation`.
- **Sensitive history retention.** `PredecessorCounts` stores only
  `Fingerprint.ID` strings (already-hashed, stable-feature identifiers
  — see [DOMAIN.md § Fingerprint](DOMAIN.md)) and integer counts —
  never raw event payloads, attributes, or any sensitive field value.
  No new sensitive data is retained beyond what `Baseline.Fingerprints`
  already holds.
- **Process-local scope, no distributed guarantee claimed.** This state
  does not survive across instances any differently than the rest of
  `Baseline` does: `InMemory` does not survive a restart, `FileStore`
  does (see the Persistence-adjacent note above) — no new claim about
  cross-instance consistency is made or implied. See ADR 0010 § "Why
  process-local."
- **Cold-start safety.** A never-before-seen transition scores as
  maximally novel, matching `categorical_novelty`'s own philosophy for
  a never-before-seen fingerprint — and is equally not, by itself,
  treated as a critical attack: `anomaly.Config.TransitionWeight`
  defaults to `0` (proven by `TestDefaultConfigTransitionWeightIsOptIn`),
  the identical "ships opt-in" mechanism `FrequencyWeight`/
  `TimePatternWeight` already established.

**`v0.6` task 026 (`transition_rarity`) extends this same threat model
with one new counter, `FingerprintStats.OutgoingTransitionTotal`, and
inherits every mitigation above unchanged rather than needing new ones:**

- **No new cardinality dimension.** `OutgoingTransitionTotal` is one
  `uint64` scalar added to the existing `FingerprintStats` entry — it
  does not add a map, and the pre-existing `maxPredecessors = 64` bound
  on `PredecessorCounts` is completely unaffected. An attacker varying
  the previous action on every call gains nothing new to exhaust beyond
  what task 025's own bound already closes.
- **Counter overflow.** `OutgoingTransitionTotal++` is a plain,
  unguarded increment — the same choice `FingerprintStats.Count` already
  made — deliberately not saturating: reaching `2^64` through legitimate
  per-event increments is not a realistic concern for any deployment's
  actual lifetime, and adding saturation logic for only this one field
  while every sibling `uint64` counter in the same struct lacks it would
  be an inconsistent special case, not a genuine safety improvement. See
  [ADR 0011 § Consequences](adr/0011-transition-rarity-statistic-and-orientation.md#consequences).
- **Cold-start / insufficient-sample uncertainty.** A frequency computed
  from a handful of observations is evidence of too little data, not
  evidence of rarity — `transition_rarity` does not fire at all below
  `OutgoingTransitionTotal_A >= Config.MinTransitionObservations`
  (default `20`, matching `MinObservations`'s own precedent), the
  identical "insufficient history is not evidence" stance
  `frequency_deviation`'s `IntervalObservations == 0` gate already
  takes. Proven by the cold-start table in
  `TestScoreTransitionRarityColdStart`. Below the gate, an attacker
  cannot force a misleadingly extreme rarity reading by keeping a
  predecessor's outgoing-transition count artificially low.
- **Baseline-poisoning resistance, inherited, not rebuilt.** Repeating a
  transition that gets `BLOCK`ed never grows
  `OutgoingTransitionTotal`/`PredecessorCounts`, for the same reason
  task 025's own counters are already immune: `Engine.Observe`'s
  pre-existing `eligibleForLearning` gate (see [Baseline
  poisoning](#baseline-poisoning) below) only learns from `ALLOW`/
  `OBSERVE_ONLY`/`ALERT` decisions, and this gate is upstream of *every*
  `Baseline.Observe` call, including the new counter's — task 026 added
  no new learning path for an attacker to target. Proven by
  `TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions`.
- **Actor/environment isolation, inherited.** `OutgoingTransitionTotal`
  lives inside the same `baseline.Key{ActorID, Environment}`-scoped
  `FingerprintStats` every other learned field already uses — no new
  code path crosses actors. Proven by
  `TestAnalyzeTransitionRarityCrossActorIsolation`.
- **Unseen vs. rare stay distinguishable.** `transition_deviation` and
  `transition_rarity` are mutually exclusive by construction
  (`count == 0` vs. `count > 0`), so an operator (or an automated
  policy) can never mistake "this has genuinely never happened" for
  "this happens, just rarely" — the two carry materially different
  security implications and are never collapsed into one signal. See
  [ADR 0011 § Preserving the unseen/rare
  distinction](adr/0011-transition-rarity-statistic-and-orientation.md#preserving-the-unseenrare-distinction).

**`v0.6` task 027 (`ngram_deviation`/`ngram_rarity`) extends this same
threat model one step further back, with two new bounded maps and one
new scalar, and again inherits every mitigation above rather than
needing new ones:**

- **Cardinality explosion, independently bounded — a genuine
  correctness subtlety this task's own review caught.**
  `TrigramCounts` (on the destination) and `TrigramContinuationTotal`
  (on the immediate predecessor) each carry their own explicit
  `maxTrigramPredecessors` (64) cap. It would be tempting to assume
  `TrigramContinuationTotal`'s cardinality is bounded "for free" by
  `PredecessorCounts`'s existing cap — it is not:
  `Baseline.Observe`'s history-window shift
  (`PreviousFingerprintID` advancing) happens unconditionally on every
  valid advance, regardless of whether `recordPredecessor` actually
  admitted the corresponding key into `PredecessorCounts` or rejected
  it for being past *that* map's own bound. An attacker varying the
  grandparent fingerprint on every call could otherwise grow either new
  map without bound even while `PredecessorCounts` itself stays capped.
  See [ADR 0012 § Why `TrigramContinuationTotal` needed its own
  bound](adr/0012-bounded-trigram-behavioral-context.md#why-trigramcontinuationtotal-needed-its-own-bound-not-an-inherited-one),
  proven by `TestBaselineObserveTrigramCountsIsBounded` and
  `TestBaselineObserveTrigramContinuationTotalIsBounded`.
- **Counter overflow.** Both new maps' counters use a plain, unguarded
  increment (`recordTrigram`/`recordTrigramContinuation`), matching
  `PredecessorCounts`/`OutgoingTransitionTotal`'s own established
  precedent — deliberately not saturating, for the identical reason:
  reaching `2^64` through legitimate per-event increments is not a
  realistic concern for any deployment's actual lifetime, and a
  special case for only these two counters would be inconsistent with
  every sibling counter in the same struct.
- **Cold-start / insufficient-sample uncertainty.** `ngram_rarity` does
  not fire at all below
  `TrigramContinuationTotal_B[A] >= Config.MinNGramObservations`
  (default `20`) — the identical "insufficient history is not
  evidence" stance `transition_rarity`'s own gate already takes, applied
  one level up. Proven by the cold-start table in
  `TestScoreNGramRarityColdStart`.
- **Baseline-poisoning resistance, inherited, not rebuilt.** Repeating
  a 3-gram that gets `BLOCK`ed never grows
  `TrigramCounts`/`TrigramContinuationTotal`, for the identical reason
  task 025/026's own counters are already immune — `Engine.Observe`'s
  pre-existing `eligibleForLearning` gate sits upstream of *every*
  `Baseline.Observe` call, including these two new maps'; task 027
  added no new learning path for an attacker to target. Proven by
  `TestObserveNGramLearnsOnlyFromEligibleDecisions`, using the exact
  scenario this section's own threat model implies: a normal
  `authenticate -> read -> update` path and a malicious, `BLOCK`ed
  `authenticate -> export -> delete` path that 50 replayed attempts
  never normalize.
- **Actor/environment isolation, inherited.** `PreviousFingerprintID`
  and both new maps live inside the same
  `baseline.Key{ActorID, Environment}`-scoped `Baseline`/`FingerprintStats`
  every other learned field already uses — no new code path crosses
  actors. Proven by `TestAnalyzeNGramCrossActorIsolation`, which checks
  not merely that no signal leaks but that a fresh actor's identical
  3-gram reads as *maximally* novel (`Value == 1`), the precise
  condition that would be violated by any leakage from another actor's
  history.
- **Unseen vs. rare stay distinguishable, one level up.**
  `ngram_deviation` and `ngram_rarity` are mutually exclusive by
  construction, for the identical reason
  `transition_deviation`/`transition_rarity` already are.
- **History-retention scope, unchanged.** `PreviousFingerprintID` is a
  single `Fingerprint.ID` string (already-hashed, stable-feature
  identifier), not a growing window — no more raw history is retained
  per actor than task 025 already introduced, just one more fixed
  pointer.

**`v0.6` task 028 (`markov_surprisal`) adds no new state at all — it
reads task 025/026's own `PredecessorCounts`/`OutgoingTransitionTotal`
fields — so it inherits every mitigation above (bounds, poisoning
resistance, actor isolation, ordering) automatically, with two
considerations specific to this signal's own arithmetic:**

- **Zero probability, made structurally impossible, not merely
  guarded.** `markov_surprisal` never evaluates a transition whose
  count is zero — that remains `transition_deviation`'s domain (task
  025) — so `-log2(0)` (`-Inf`) is never computed at all, not
  clamped after the fact. Combined with the shared
  `MinTransitionObservations` gate (below which no signal fires),
  `frequency` is always in `(0, 1]` whenever this signal's arithmetic
  runs, making `surprisal` always finite and `normalized` always in
  `[0, 1)`. Proven by `TestScoreMarkovSurprisalUnseenTransitionNeverFires`
  and `TestScoreMarkovSurprisalNeverExceedsBounds` (the latter across
  totals up to 10,000, checking `!IsNaN`/`!IsInf`/bounds explicitly on
  every reading, not just the typical case).
- **Correlated-signal amplification, prevented structurally, not by
  convention.** `transition_rarity` and `markov_surprisal` are
  mathematically proven (see [ADR
  0013](adr/0013-first-order-markov-surprisal-without-duplicate-evidence.md))
  to be monotonic reparameterizations of the *identical* frequency
  statistic — scoring both independently would double-count one piece
  of evidence as if it were two, inflating `Score` beyond what the
  underlying evidence actually supports. `anomaly.Score` forces
  `transition_rarity`'s own contribution to zero whenever
  `Config.MarkovWeight > 0`, regardless of what
  `Config.TransitionRarityWeight` is separately set to — a caller
  cannot double-count this evidence by any combination of the two
  weight fields, because the code itself enforces the exclusion, not
  documentation alone. Proven by
  `TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring`
  (`Score` with both weights set is bit-for-bit identical to `Score`
  with only `MarkovWeight` set). `markov_surprisal` *can* legitimately
  co-fire with task 027's `ngram_deviation`/`ngram_rarity` on the same
  event — those answer a genuinely different question (a specific
  two-fingerprint predecessor pair, not the same first-order frequency)
  — and `combine()`'s existing clamped noisy-OR keeps that combination
  bounded regardless, proven by
  `TestScoreCombinedMarkovAndNGramSignalsRemainBounded`.

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

### Configuration-input validation

**Threat:** a config-time typo silently *weakens* a policy rather than
visibly breaking it. Unlike a malformed `Event` (rejected immediately
at the boundary) or an entirely unconfigured `Policy` (fails closed to
`BLOCK`), a `PolicyCondition` field with a typo'd value — `actor_type:
srevice` instead of `service` — compiles into a perfectly well-formed
`policy.Condition` that simply never matches anything. The rule
silently stops firing; nothing about evaluation itself looks wrong, so
an operator who believes a rule is active has no signal it never was.

**Status: implemented**
([task 019](tasks/019-policy-config-model.md), `config` package).
`(PolicyConfig).Validate()` — called unconditionally inside
`CompilePolicy` too, so this protection cannot be bypassed by skipping
an explicit validation step — rejects, with a field-path-identifying
error: an unsupported/missing schema version, a missing or invalid
default decision/reason, an invalid decision on any rule, an invalid
`actor_type`/`operation_category`/`min_risk_level` on any condition
(including inside `Unless`), an empty or duplicate rule name, an empty
reason, an overlong name, and more than `1000` rules. This is
deliberately *stricter* than `policy.Policy.Evaluate`'s own runtime
fail-closed behavior for exactly this reason: a same-shaped runtime
failure has no way to point back at the config file line that caused
it, while a config-time validation error does. A `PolicyCondition`
with every field left at its zero value is explicitly *not* rejected —
that is legitimate "matches everything" catch-all semantics inherited
from `policy.Condition` itself, not a mistake to flag.

**Resource bounds are deliberately stricter than runtime bounds
elsewhere in this codebase.** `maxRules = 1000` and
`maxNameLength = 256` are far smaller than, e.g., the 100,000-key
`Attributes` map this document's own resource-exhaustion tests already
probe without panicking — a policy config file is human-authored
operational input, not per-event telemetry volume, so a much stricter
cap is appropriate and does not constrain any real use case.

**Out of scope by construction:** `NaN`/`Inf` and numeric-range
validation do not apply to `PolicyCondition` today, since it has no
numeric matcher field (`MinRiskLevel` is a qualitative string enum,
not a float) — `internal/policy.Condition` itself would need a numeric
matching dimension before this validation surface grows to cover it.

**File parsing** ([task 020](tasks/020-policy-config-loader.md),
`config.Load`/`config.LoadFile`) introduced the threats a file format
adds beyond `Validate()`'s own checks, all implemented, not deferred:

- **Unknown-field handling is strict, unconditionally.** `Load` uses
  `go.yaml.in/yaml/v3`'s `Decoder.KnownFields(true)` — an unrecognized
  field anywhere in the document (top-level or nested inside a rule's
  `when:`/`unless:`) fails with an error naming it, rather than being
  silently ignored. This is the file-format-level version of the same
  threat the paragraphs above address for value-level typos: a
  field-name typo must not silently produce different security
  behavior than the operator intended. There is no configuration
  option to relax this.
- **Duplicate mapping keys are rejected** — verified as
  `go.yaml.in/yaml/v3`'s own default `Decoder` behavior via a real
  test (`TestLoadRejectsDuplicateYAMLKeys`) against the actual
  library, not assumed from its documentation. A document with two
  `decision:` keys in the same rule fails to decode at all, rather
  than silently taking the first or last value.
- **Input size is bounded.** `LoadFile` reads at most 1 MiB
  (`io.LimitReader(f, maxConfigFileSize+1)`, with the `+1` used to
  *detect* an over-limit file rather than silently truncate and parse
  a partial document) — bounding a pathological input (an
  accidentally-huge file, a special device file) to a fixed, cheap
  read. `Load` itself, given an in-memory `[]byte`, has no additional
  size bound beyond what the caller already chose to hold in memory.
- **Empty input is a distinct, actionable error** (`ErrEmptyInput`),
  not a bare `io.EOF` leaked from the decoder.
- **A fuzz target** (`FuzzLoad`) asserts the one invariant a parser
  handling untrusted input must have: arbitrary bytes never panic
  `Load`. Run with `go test ./config -fuzz=FuzzLoad`.
- **No secret leakage in errors.** Loader/validation errors identify
  field paths and offending values (e.g. an invalid decision string),
  never a signing secret or credential — this loader has no such field
  to leak in the first place (`PolicyConfig` carries no secret-shaped
  data; `AlertConfig`, introduced by task 023 below, doesn't either —
  it has no delivery/credential fields at all, since delivery
  configuration is explicitly out of that task's scope).

### Alert configuration-input validation

**Threat:** the identical config-time-typo threat the section above
addresses for `PolicyCondition`, applied to `AlertConditionConfig`: a
typo'd `severity: crital` or `actor_type: srevice` would otherwise
compile into a well-formed `alert.Condition` that simply never
matches, silently disabling a rule an operator believes is active.
Alert Evaluation's own "no match means no alert" design (see
[Policy bypass](#policy-bypass) above and
`alert.Evaluate`'s doc comment) makes this threat *more* dangerous
here than for Policy, not less — there is no fail-closed floor at
runtime to fall back on if a rule silently stops matching; the
observable failure mode is simply the total *absence* of an alert that
should have fired.

**Status: implemented** ([task
023](tasks/023-declarative-alert-configuration.md), `config` package —
`AlertConfig`/`CompileAlerts`/`LoadAlerts`/`LoadAlertsFile`, a
deliberately **separate** document and compilation path from
`PolicyConfig`/`CompilePolicy`, not a shared one — see [ADR
0009](adr/0009-alert-config-is-a-separate-document.md)).
`(AlertConfig).Validate()` — called unconditionally inside
`CompileAlerts`, same non-bypassable guarantee `PolicyConfig.Validate`
already has — rejects: an unsupported schema version, an invalid
`severity` on any rule, an invalid `decision`/`min_risk_level`/
`actor_type`/`target_category` on any condition, an empty or duplicate
rule name, an overlong name, more than `1000` rules (the same bound
`PolicyConfig` uses), and — the one validation surface `PolicyCondition`
explicitly does *not* need (it has no numeric matcher) —
`min_anomaly_score`/`max_trust_score` values that are `NaN`, `±Inf`,
negative, or greater than `1`: `alert.Condition.MinAnomalyScore`/
`MaxTrustScore` are always meant to be compared against
`Anomaly.Score`/`Trust.Score`, both of which are documented to stay
within `[0, 1]`, so a threshold outside that range could never match
anything meaningful and is rejected as a config mistake rather than
silently accepted as a threshold that can never fire or always fires.
Same file-parsing threats as `PolicyConfig` (unknown-field rejection,
duplicate-key rejection, bounded read, `FuzzLoadAlerts`) are covered
identically, reusing the exact same `go.yaml.in/yaml/v3` decoding path
— not a second, parallel parser.

Unlike `PolicyConfig`, `AlertConfig` has no `default_decision`/
`default_reason` to validate — an empty `Rules` list is a legitimate,
if inert, `AlertConfig`, matching `alert.Evaluate`'s own documented
asymmetry with `policy.Policy.Evaluate` (see [Policy
bypass](#policy-bypass)): the *absence* of any alert
rule is a safe, observable no-op, not a security regression the way an
unconfigured `Policy` silently falling open would be.

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

`v0.6`'s new `FingerprintStats.PredecessorCounts` (see [Sequence
state](#sequence-state) above) is a deliberate exception to "unbounded
by design" above: unlike `Baseline.Fingerprints` itself, it *is*
capped (64 distinct entries), specifically because it compounds the
existing unbounded-map characteristic with a second, per-entry
dimension an attacker could otherwise inflate independently by varying
the *previous* action on every call.

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

### Alert/notification delivery integrity

**Threat:** once a `Decision` can produce an externally-delivered
`Alert` (webhook, chat, paging system — see
[`trustvian-project-spec.md` § 18](../trustvian-project-spec.md#18-alert--notification-system)),
a forged, replayed, or tampered delivery could make an external system
act on a notification Trustvian never actually sent, or fail to notice
a real one was dropped.

**Status: implemented for the one delivery mechanism this stage ships
(`alert.WebhookSink`, [task
018](tasks/018-alert-notification-foundation.md)).** `NewWebhookSink`
fails closed at construction — not silently at the first `Send` — for
a non-HTTPS destination (`ErrNonHTTPSDestination`,
`TestNewWebhookSinkRejectsNonHTTPS`), a literal loopback/link-local
destination unless explicitly overridden
(`ErrLoopbackDestination`/`WithAllowLoopback`,
`TestNewWebhookSinkRejectsLoopbackDestination`), or a missing signing
secret (`ErrMissingSecret`). Every delivery is signed with
HMAC-SHA256 computed over `"<unix-timestamp>.<payload-body>"`, not the
body alone — binding the timestamp into the signed content is what lets
a receiver enforce a replay window without an attacker being able to
attach a fresh timestamp to a previously-valid signature — verified by
`TestSendSignsPayloadCorrectly` (an independently recomputed HMAC
matches the `X-Trustvian-Signature` header exactly) and
`TestSendTamperedPayloadFailsVerification` (flipping one payload byte
changes the recomputed signature). `TestSendDoesNotLeakSecret` proves
the raw signing secret never appears in the outbound body or any
header. A bounded request timeout (`TestSendRespectsTimeout`) and a
bounded payload size, `ErrPayloadTooLarge`
(`TestSendPayloadTooLargeMakesNoNetworkCall` — the oversized-payload
case makes zero network calls, not merely returns an error after
sending) close the resource-exhaustion angle a webhook to an
operator-configured, potentially attacker-influenced destination would
otherwise open. See [DOMAIN.md § Alert](DOMAIN.md#alert) for the domain
model and [ADR 0007](adr/0007-alert-package-is-public.md) for why
`alert` is a public package.

**What remains deliberately unimplemented:** delivery retry, a
delivery-state/dead-letter mechanism, and deduplication are the
separately-scoped Reliability stage's job (see
[ROADMAP.md § Alert & Notification
phase](ROADMAP.md#alert--notification-phase)), not this one's — a
failed or dropped delivery today simply returns an error to the caller,
with no automatic recovery. Full SSRF protection (DNS-resolution-based
destination validation, not just literal-IP loopback checks) is
explicitly out of scope for the same reason `docs/ARCHITECTURE.md`
already draws this boundary for the rest of Trustvian: Trustvian is not
itself a network-egress enforcement point, and real SSRF protection
belongs at the deploying application's network layer.

### AI Agent behavioral security

`v0.7` ([task 014](tasks/014-ai-agent.md), [ADR
0014](adr/0014-ai-agents-as-first-class-behavioral-actors.md))
introduces no new state, no new pipeline stage, and no agent-specific
detector — an AI agent is `Actor{Type: ActorTypeAIAgent}`, scored by
the identical engine every other actor already uses. The threats below
are organized around that fact: most are already covered by mechanisms
this document already describes for actors in general, applied here to
the AI-agent case specifically; a few are explicitly future work.

- **High-cardinality session IDs (baseline/fingerprint exhaustion).**
  **Status: implemented.** `Context.SessionID` never enters
  `features.StableFeatures`, `Fingerprint`, or `baseline.Key` — an
  attacker (or a legitimately chatty agent) generating an unbounded
  number of distinct session IDs cannot create a corresponding number
  of distinct behavioral identities or baselines this way, because
  session identity and behavioral identity are structurally different
  dimensions. Proven at scale, not just by omission:
  `TestAnalyzeAgentSessionIDDoesNotExplodeBaseline` runs 1,000 events
  with 1,000 distinct `SessionID` values and confirms exactly one
  `Fingerprint` entry accumulates all 1,000 observations.
- **Prompt/tool-argument data leakage into behavioral state.**
  **Status: implemented, by construction.** Trustvian's `Event` model
  has no field for raw prompt text, completion text, or tool argument
  values, and this task adds none. Tool identity
  (`Operation.Name`/`Target.Name`) is documented as required to be a
  stable, low-cardinality string — never argument content — the
  identical "no raw payload in fingerprint identity" principle this
  document already applies to `Attributes` generally (see [Resource
  exhaustion](#resource-exhaustion) above), restated explicitly for
  AI-agent producers, who are the party most likely to have
  prompt/argument data on hand to (mis)use this way. There is no
  mechanism in this module that could retain such data even if a
  careless producer put it in `Target.Name` — it would simply become
  (and stay) part of that Fingerprint's identity, a data-hygiene
  problem for the producer to avoid, not something this module
  redacts after the fact.
- **Agent identity spoofing / identity mismatch.** **Status: same
  boundary as every other actor, not agent-specific.** `Actor.ID` +
  `IdentityConfidence` are inputs Trustvian trusts, not something it
  authenticates (see [`.claude/rules/security.md` § Identity is an
  input, not a computation](../.claude/rules/security.md)) — an AI
  agent is no different from a service or user actor in this respect.
  Verifying the calling agent's real identity is the deploying
  application's authentication layer's job, upstream of Trustvian.
- **Delegation abuse** (an attacker forging `DelegatedFrom` to make an
  unauthorized action appear delegated from a trusted agent).
  **Status: recorded, not yet defended — future work, honestly
  labeled.** `DelegatedFrom` is presently an unauthenticated,
  self-reported field with zero scoring effect
  (`TestAnalyzeAgentDelegationContextScoredIdentically` proves this
  directly) — nothing in this task treats it as a trust signal, so
  nothing can be tricked by a forged value *today*, but nothing
  verifies it either. A future task that builds a delegation-aware
  detector or `Policy` condition must treat `DelegatedFrom` as
  unauthenticated input requiring its own verification, not as
  something this task already secures.
- **Approval self-assertion** (an agent's own event claiming
  `ApprovalStatus = Approved` and having that trusted merely because
  the event says so). **Status: `ApprovalStatus` now has a real
  consumer ([task 030](../tasks/030-approval-aware-policy-semantics.md),
  [ADR 0015](adr/0015-approval-as-policy-evidence-not-behavioral-anomaly.md)) —
  the trust boundary below is enforced by construction, not merely
  documented, but provenance verification itself remains future
  work.** `policy.Condition.ApprovalStatus` lets a `Policy` require
  `Approved` for a given operation, via the existing `Unless`
  mechanism — but `ApprovalStatus` is still exactly what it was before
  this task: an unauthenticated, self-reported field. Task 030 adds no
  cryptographic verification, no OAuth/IAM check, and no call to an
  external authorization system — it only makes the *evaluation* of
  that (still-untrusted) evidence deterministic and explainable. Two
  guarantees are enforced, not aspirational, proven by test:
  - **The event cannot define its own requirement.** An event
    self-declaring `ApprovalNotRequired` cannot exempt itself from a
    `Policy` rule that requires `Approved` — only the existence of the
    rule, authored in `Policy`, determines whether approval is
    required at all. Proven by
    `TestEvaluateApprovalPolicyAuthorityEventCannotOverridePolicy`.
  - **Missing evidence fails closed.** `ApprovalUnspecified` (no
    evidence recorded) is treated the same as an explicit `Denied` —
    `BLOCK`, never a silent pass. Proven by
    `TestEvaluateApprovalFailSafeOnMissingEvidence`.

  What remains genuinely future work, same shape as delegation abuse
  above: verifying that a given `ApprovalStatus = Approved` value
  actually originated from a trusted human/authorization system, as
  opposed to the event producer's own unverified claim. An AI agent
  today can still self-assert `Approved` and have `Policy` accept that
  evidence at face value — Trustvian evaluates the evidence it is
  given; it does not (yet, and not as part of task 030) verify where
  that evidence came from. A future task adding provenance
  verification (e.g., a signed assertion from a specific
  authorization system) is a distinct, larger piece of work this task
  deliberately did not build ahead of a concrete need.
- **Tool abuse (unexpected/rare tool usage).** **Status: implemented,
  via existing signals.** `categorical_novelty` and
  `transition_deviation`/`transition_rarity` already flag a tool an
  agent has never (or rarely) used — proven for the "never used" case
  by `TestAnalyzeAgentToolNoveltyDetectedByExistingEngine`. No new
  detector was built or is needed.
- **External exfiltration path (a sensitive read followed by an
  outbound call).** **Status: implemented, via existing `v0.6`
  sequence signals — the task's own central proof.**
  `TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine` shows
  `ngram_deviation` (task 027) detecting exactly this shape:
  `search -> secret.read -> external.post` flagged as novel even
  though both individual hops (`search -> secret.read`,
  `secret.read -> external.post`) are independently familiar. Marking
  a specific destination as always-sensitive regardless of
  familiarity is `anomaly.Config.SensitiveTargetFloor`'s existing job
  (see [Malicious agents / privilege
  escalation](#malicious-agents--privilege-escalation) above) —
  unchanged, and already applicable to agent-sourced events with zero
  modification.
- **Baseline poisoning via repeated malicious tool-call sequences.**
  **Status: implemented, inherited.** Every new field this task adds
  is context-only and never mutates learned state on its own; the
  existing `eligibleForLearning` gate ([Baseline
  poisoning](#baseline-poisoning) above) already governs whether *any*
  event — agent-sourced or not — is eligible to update a `Baseline`.
  This task introduces no new learning path and therefore no new way
  to bypass that gate.
- **Rapid tool-call state exhaustion (an agent issuing tool calls far
  faster than a human-driven actor would).** **Status: bounded by
  existing, pre-agent mechanisms.** Every behavioral-state structure a
  rapid-fire agent could grow (`PredecessorCounts`, `TrigramCounts`,
  `TrigramContinuationTotal`) is already independently bounded at 64
  entries (tasks 025/027) regardless of call *rate* — a fast agent
  fills the same bounded structures faster, it does not grow them
  larger. `frequency_deviation` (task 004) is the existing,
  general-purpose signal for anomalously high call rates; no
  agent-specific rate limiting exists or was added.
- **Agent-to-agent graph analytics** (mapping delegation relationships
  across many agents to find escalation or collusion patterns).
  **Status: explicitly future work, not built.** `DelegatedFrom`
  records one hop; no graph, no multi-hop traversal, no relationship
  analytics exists. See [ADR 0014](adr/0014-ai-agents-as-first-class-behavioral-actors.md)'s
  "Delegation: one hop, no graph" section.

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
