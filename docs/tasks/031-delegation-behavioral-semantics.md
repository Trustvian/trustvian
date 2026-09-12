# 031 — Delegation Behavioral Semantics

**Milestone:** v0.7 · **Depends on:** [014](014-ai-agent.md) (adds
`event.Context.DelegatedFrom`, this task's first real consumer);
independent of [030](030-approval-aware-policy-semantics.md) — proven,
not just asserted, by
`TestAnalyzeDelegationApprovalIndependence` · **Blocks:**
[032](#roadmap-successors-illustrative) (Agent Security Scenario
Validation combines this task's signal with 030's policy mechanism).

## Objective

Answer this task's own design question with code and tests, not just
a design doc: *can Trustvian identify unusual delegation relationships
using bounded behavioral learning without treating self-reported
delegation metadata as authenticated provenance?* See [ADR
0016](../adr/0016-delegation-as-behavioral-evidence-not-provenance.md)
for the full design.

## Why

Task 014 added `DelegatedFrom` explicitly reserved for "a later
signal to consume once one has a concrete design" — this is that
design. Two questions had to be answered correctly before any code was
written, both genuinely dangerous to get wrong:

1. **Behavioral vs. provenance.** "Does Agent B normally receive
   delegated work from Agent A?" (a question bounded behavioral
   learning can answer) is a completely different question from "did
   Agent A cryptographically/authentically delegate this action?" (a
   question this task does not, and cannot, answer). Blurring them —
   treating behavioral familiarity as authorization — would let a
   spoofed `DelegatedFrom` value "normalize" itself into trust through
   repetition, exactly the vulnerability this task's brief named
   explicitly.
2. **Reuse vs. new state.** `DelegatedFrom` cannot enter `Fingerprint`/
   `baseline.Key` (task 014's own established rule), so it needed
   *some* new state to be learnable at all — the question was how
   little new state, reusing which existing mechanism.

## Scope

```text
Event.Context.DelegatedFrom (unchanged, task 014)
      ↓
features.Extract reads it into VolatileFeatures.DelegatedFrom
(Stable unaffected — Fingerprint identity untouched)
      ↓
baseline.Baseline.Observe records it into DelegatorCounts
(actor-level, bounded at 64 distinct delegators — maxDelegators)
      ↓
anomaly.Score's delegationSignal reads DelegatorCounts
(binary seen/unseen, mirroring transitionSignal — no rarity signal)
      ↓
Trust / Policy / Decision / Alert — all unchanged, proven independent
of Anomaly.Score/Fingerprint by test
```

- `internal/features.VolatileFeatures` gains `DelegatedFrom string`;
  `Extract` populates it from `e.Context.DelegatedFrom`.
- `internal/baseline.Baseline` gains `DelegatorCounts map[string]uint64`,
  bounded at a new `maxDelegators = 64` constant (its own independent
  bound, not shared with `maxPredecessors`, for the identical reason
  `maxTrigramPredecessors` is independent). Updated unconditionally in
  `Observe` whenever `vol.DelegatedFrom != ""` — no ordering guard,
  since delegation provenance carries no sequence dependency.
- `internal/anomaly.Config` gains `DelegationWeight float64` (defaults
  to `0`, opt-in — identical precedent to every `v0.6`/`v0.7` signal
  weight). New signal `delegationSignal` → `Signal{Name:
  "delegation_deviation", ...}`, gated on
  `feat.Volatile.DelegatedFrom != ""`, evaluated independently of
  `Fingerprint` maturity (it reads `bl.DelegatorCounts` directly, not
  anything inside `FingerprintStats`).
- **No change to `store.Store`'s interface or `Baseline.Observe`'s
  signature.** `DelegatedFrom` flows through the existing `vol`
  parameter both already take — see ADR 0016's own section on why
  this avoided a 100+-call-site blast radius.

## Non-Goals

- **No delegation graph, no multi-hop chain, no `DelegationID`/
  `DelegationDepth`.** A single immediate delegator only, matching
  task 014's own "one hop, no graph" scope.
- **No delegation *rarity*/probability signal, no delegation Markov,
  no delegation n-gram.** `delegation_deviation` is binary seen/unseen
  only — the minimum meaningful evidence this task's brief asked for,
  explicitly avoiding `v0.6`'s multi-correlated-signal pattern without
  a proven need for it here.
- **No provenance verification of any kind** — no cryptographic
  signature check, no OAuth/IAM integration, no trusted-orchestration
  call. `DelegatedFrom` remains exactly as unauthenticated after this
  task as before it.
- **No coupling of delegation evidence to approval evidence
  (task 030) or to `Actor.IdentityConfidence`.** All three stay
  independent, proven by dedicated tests, not merely by omission.
- **No new `Store.Observe`/`Baseline.Observe` parameter, no new
  `store.Store` port method, no `DelegationEngine`/`DelegationGraph`/
  `DelegationStore`/`DelegationBaseline`/`AgentGraph`.**
- **No session-scoped or persisted approval-style caching of
  delegation state.** Delegation learning follows the identical
  per-event, gated `Analyze`/`Observe` lifecycle every other
  behavioral dimension already uses.

## Technical Requirements

- `DelegatedFrom` must never affect `StableFeatures`/`Fingerprint.ID` —
  proven by `TestExtractPopulatesDelegatedFromIntoVolatileNotStable`,
  `TestScoreDelegationDeviationDoesNotAffectFingerprintOrStable`, and
  `TestAnalyzeDelegationFingerprintStability`.
- `DelegatorCounts` must be bounded at exactly 64 distinct entries
  regardless of how many distinct delegators are observed — proven at
  200 distinct delegators (well beyond the bound) by
  `TestBaselineObserveDelegatorCountsIsBounded`.
- `DelegatorCounts` must be actor+environment-isolated
  (`baseline.Key`-scoped) — proven by
  `TestAnalyzeDelegationActorIsolation`.
- An event with no `DelegatedFrom` must leave `DelegatorCounts`
  completely untouched and produce no `delegation_deviation` signal —
  proven by `TestBaselineObserveMissingDelegationDoesNotUpdateDelegatorCounts`
  and `TestAnalyzeDelegationMissingDelegationUnaffected`.
- Delegation learning must respect the existing `eligibleForLearning`
  gate — a BLOCKed event's claimed delegator must never become
  "familiar" through repetition — proven by
  `TestAnalyzeDelegationPoisoningIneligibleEventsDoNotTrain`.
- `Engine.Analyze` must remain read-only with respect to delegation
  state — proven by `TestAnalyzeDelegationScoreBeforeLearn`.
- Delegation and approval evidence must never entangle — proven by
  `TestAnalyzeDelegationApprovalIndependence`.
- Zero new dependencies; zero changes to `store.Store`'s interface,
  `internal/policy`, `internal/trust`.

## Tests

`internal/features/features_test.go` (+1):
`TestExtractPopulatesDelegatedFromIntoVolatileNotStable`.

`internal/baseline/baseline_test.go` (+4):
`TestBaselineObserveTracksDelegatorCounts`,
`TestBaselineObserveMissingDelegationDoesNotUpdateDelegatorCounts`,
`TestBaselineObserveDelegatorCountsIsImmutable`,
**`TestBaselineObserveDelegatorCountsIsBounded`** (the mandatory
cardinality-attack proof — 200 distinct delegators, bounded at 64).

`internal/anomaly/anomaly_test.go` (+5):
`TestDefaultConfigDelegationWeightIsOptIn`,
`TestScoreDelegationDeviation` (table: familiar/novel/absent/cold-start),
`TestScoreDelegationDeviationDoesNotAffectFingerprintOrStable`,
`TestScoreMatchesDocumentedNoisyOrFormulaWithDelegationSignal`.

`engine_test.go` (+8):
`TestAnalyzeDelegationNoveltyDetectedByExistingSignal`,
**`TestAnalyzeDelegationScoreBeforeLearn`** (the mandatory
score-before-learn regression),
**`TestAnalyzeDelegationPoisoningIneligibleEventsDoNotTrain`** (the
mandatory poisoning regression),
`TestAnalyzeDelegationActorIsolation`,
`TestAnalyzeDelegationMissingDelegationUnaffected`,
`TestAnalyzeDelegationFingerprintStability`,
**`TestAnalyzeDelegationApprovalIndependence`** (the mandatory
delegation/approval orthogonality regression).

All of the above run under `go test ./... -race -count=1`.

## Benchmarks

`internal/baseline/baseline_bench_test.go`: no dedicated new benchmark
— `DelegatorCounts`'s update cost is structurally identical to
`recordPredecessor`'s own already-benchmarked copy-on-write path
(`BenchmarkObserveTransition`).

`engine_bench_test.go` (+3): `BenchmarkEngineAnalyzeDelegationAbsent`
(confirmed `456 B/op, 17 allocs/op` — byte-for-byte identical to
`BenchmarkEngineAnalyze`, proving zero cost when no event carries
`DelegatedFrom`), `BenchmarkEngineAnalyzeDelegationFamiliar`,
`BenchmarkEngineAnalyzeDelegationNovel`. See
[docs/PERFORMANCE.md § v0.7 task
031](../PERFORMANCE.md#v07-task-031-delegation-behavioral-semantics).

## Documentation

- [docs/adr/0016-delegation-as-behavioral-evidence-not-provenance.md](../adr/0016-delegation-as-behavioral-evidence-not-provenance.md)
  (new): the full design, including why no `Store.Observe` signature
  change was needed.
- [docs/DOMAIN.md](../DOMAIN.md): `DelegatedFrom`'s entry extended with
  its new consumer, orientation, and cold-start semantics.
- [docs/SECURITY.md](../SECURITY.md): "Delegation abuse" entry updated
  to reference this task's actual mechanism and its explicit
  non-authentication limitation.
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md): `Baseline`'s state
  diagram extended to show `DelegatorCounts` alongside
  `PredecessorCounts`/`TrigramCounts`, still inside the existing
  behavioral subsystem, not a new one.
- [trustvian-project-spec.md](../../trustvian-project-spec.md) § 16:
  bounded delegation behavioral learning marked implemented;
  delegation authentication/authorization marked explicitly not
  implemented.
- [docs/ROADMAP.md](../ROADMAP.md) § v0.7: task 031 marked done; 032/033
  remain scoped, not implemented.
- [README.md](../../README.md) / [CHANGELOG.md](../../CHANGELOG.md):
  this task's capability, accurately scoped.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new test above.
- `TestBaselineObserveDelegatorCountsIsBounded` passes — the mandatory
  cardinality-attack proof.
- `TestAnalyzeDelegationPoisoningIneligibleEventsDoNotTrain` and
  `TestAnalyzeDelegationScoreBeforeLearn` both pass — delegation state
  obeys the identical learning-eligibility contract every other
  behavioral dimension already does.
- `TestAnalyzeDelegationApprovalIndependence` passes — delegation and
  approval evidence never entangle.
- `TestAnalyzeDelegationFingerprintStability` and
  `TestScoreDelegationDeviationDoesNotAffectFingerprintOrStable` both
  pass — `DelegatedFrom` never affects `Fingerprint` identity.
- No new package, no new pipeline stage, no new `Store.Observe`
  parameter, no new dependency — verified by reviewing the diff against
  this constraint (`git diff -- go.mod go.sum processor/go.mod
  processor/go.sum` empty).
- `BenchmarkEngineAnalyzeDelegationAbsent` confirms zero added
  allocation when no event carries `DelegatedFrom`.
- CLI and `processor/` require zero code changes — verified directly
  (none were made).
