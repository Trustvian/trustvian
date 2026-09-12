# 0016 — Delegation as behavioral evidence, not provenance

## Context

[Task 014](../tasks/014-ai-agent.md) added `event.Context.DelegatedFrom`
as a typed, optional field — the immediate delegator's `Actor.ID` for
a single agent-to-agent delegation hop — deliberately unconsumed: no
signal read it, no `Policy` condition matched on it. [Task
031](../tasks/031-delegation-behavioral-semantics.md) gives it its
first real consumer. Before writing any code, this task's own brief
posed the central design question directly: can Trustvian identify
unusual delegation relationships using bounded behavioral learning,
without treating self-reported delegation metadata as authenticated
provenance? Getting the second half wrong — quietly treating a
familiar delegator as an authorized one — would be a security defect
disguised as a convenience.

## Decision: one bounded, actor-level behavioral signal, no provenance verification

`DelegatedFrom` answers a *behavioral* question — "does this actor
normally receive delegated work from this delegator?" — never a
*provenance* question — "did this delegator cryptographically or
authentically delegate this action?" Task 031 answers only the first.
Concretely:

- `internal/features.VolatileFeatures` gains one field,
  `DelegatedFrom string`, read from `event.Context.DelegatedFrom` in
  `Extract` — the identical "context flows into Volatile, never
  Stable" precedent `Error`/`HasLatency` already establish.
- `internal/baseline.Baseline` gains one bounded map,
  `DelegatorCounts map[string]uint64` (capped at 64 distinct entries,
  `maxDelegators`), updated unconditionally in `Observe` whenever
  `vol.DelegatedFrom != ""`.
- `internal/anomaly` gains one opt-in signal, `delegation_deviation`
  (`Config.DelegationWeight`, defaults to `0`): binary seen/unseen,
  mirroring `transitionSignal`'s exact shape.
- Nothing else. No `DelegationEngine`, `DelegationGraph`,
  `DelegationStore`, `DelegationBaseline`, or `AgentGraph` exists or
  is created.

This mirrors task 014's own "extend, don't parallel" decision, and
task 030's "one more field on an existing mechanism, not a new one" —
the third time this module has extended an existing bounded-map
pattern (`PredecessorCounts`, `TrigramCounts`, now `DelegatorCounts`)
rather than inventing a new kind of state.

## Orientation: `DelegatorCounts` is actor-level, not per-operation

The task's own brief asked this explicitly: is the learned
relationship "delegator → delegatee" or "delegatee received from
delegator", and should it be scoped per-operation or per-actor? The
answer chosen, and the reason it differs from `PredecessorCounts`'s
own placement:

```
current actor = delegatee (Actor.ID on the Event)
DelegatedFrom = immediate delegator

Question answered: for THIS actor, how familiar is this delegator?
```

`DelegatorCounts` lives directly on `Baseline` (like
`LastFingerprintID`/`PreviousFingerprintID`), not inside
`FingerprintStats` (like `PredecessorCounts`), because "who normally
delegates to this actor" is a property of the actor as a whole, not
of any one operation it happens to perform. An actor delegated
`search`, `read`, and `shell.execute` all by the same orchestrator has
one behavioral relationship with that orchestrator, not three
independent ones fragmented across its own operations — fragmenting
it per-Fingerprint would multiply cardinality for no behavioral
benefit and make "is this delegator new to this actor at all"
unnecessarily harder to answer.

## Why this needed no change to `Store.Observe`'s signature

Sequence state (`PredecessorCounts`, `TrigramCounts`) never needed a
new `Store.Observe` parameter because it is *derived* from a
`Baseline`'s own prior state (`LastFingerprintID`,
`PreviousFingerprintID`) — the caller supplies none of it directly.
`DelegatedFrom` is different: it is a fact about *this specific
event* that cannot be derived from anything already stored, so it
must reach `Baseline.Observe` somehow. The chosen path — adding it to
`features.VolatileFeatures`, which `Baseline.Observe` already reads —
means `store.Store`'s interface, `store.InMemory.Observe`,
`store.FileStore.Observe`, and `baseline.Baseline.Observe`'s own
*signature* all stay byte-for-byte unchanged; only their read-through
of an existing parameter's newly-added field changes. The alternative
(adding a `delegatedFrom string` parameter directly to `Observe`)
would have required updating every one of this module's 100+ existing
`Observe(fp, vol, now)` call sites across `internal/baseline`,
`internal/store`, and `internal/anomaly`'s own test suites — a
disproportionate blast radius for one new optional input, and exactly
the kind of unrelated churn CLAUDE.md's "implement only the smallest
vertical slice" and "never rewrite working code unnecessarily"
guidance warns against. `VolatileFeatures` already exists precisely
to carry "per-event signals that feed anomaly detection but never
join a stable Fingerprint" — `DelegatedFrom` is a textbook fit, not a
workaround.

## No minimum-support gate — this signal deliberately mirrors `transitionSignal`, not `transitionRaritySignal`

`delegation_deviation` has no `MinDelegationObservations` field of its
own. This is a deliberate choice, not an oversight: it answers "seen
before or not" (binary), the identical question `transitionSignal`
already answers with no minimum-support gate of its own — cold start
for a binary seen/unseen signal is handled by the `Anomaly.Confidence`
this package's own doc comment already describes as the *general*
cold-start mechanism (driven by the destination Fingerprint's own
maturity via `categorical_novelty`), not by a second, signal-specific
threshold. A `MinTransitionObservations`-style gate belongs to a
*rarity* signal (a frequency estimate, which needs a minimum sample
size to be trustworthy) — task 031 deliberately does not build a
`delegation_rarity` signal, so it needs no such gate. If one is ever
justified by real traffic, it would need its own minimum-support field
the way `TransitionRarityWeight` needed
`MinTransitionObservations` — future work, not this task's.

## `DelegatedFrom` remains untrusted, self-reported evidence

This is the second design question the task's own brief demanded be
answered honestly. Nothing in this task verifies that
`Context.DelegatedFrom` actually names the real delegator — it is
exactly as unauthenticated after this task as before it. Two
consequences, both proven by test, not just documented:

- **A familiar delegator is not thereby authorized.**
  `delegation_deviation` measures behavioral familiarity, nothing
  more. A malicious actor B can set `DelegatedFrom = "trusted-agent-A"`
  and, once repeated enough times through eligible (non-blocked)
  events, that claim becomes behaviorally "familiar" — Trustvian has
  no way to know the claim is false, because nothing in this task (or
  any prior one) authenticates it. This is the identical spoofing
  threat `docs/SECURITY.md`'s "Delegation abuse" entry already
  documented for task 014's `DelegatedFrom`, now stated again
  precisely because task 031 is the first task that could plausibly be
  mistaken for having addressed it.
- **An unfamiliar delegator is not thereby malicious.** A legitimate
  new integration, a newly onboarded orchestrator, or a one-off
  administrative delegation all produce the identical
  `delegation_deviation` signal a spoofed one would. This is anomaly
  evidence — "is this unusual?" — never a malice verdict, the same
  distinction `docs/SECURITY.md`'s "Explainability" principles already
  hold every other signal in this module to.

**Trusted provenance verification (signed delegation claims,
authenticated runtime metadata, a trusted orchestration layer,
identity-provider evidence) remains explicitly future, out-of-scope
integration work** — mentioned here as a future possibility, not
designed or implemented by this task.

## Delegation and approval evidence stay independent

Task 030 established `ApprovalStatus` as policy evidence, entirely
separate from behavioral scoring. Task 031 preserves that separation
in the other direction: `delegation_deviation` never reads
`ApprovalStatus`, and no approval-aware `Policy` condition reads
`DelegatorCounts` or `delegation_deviation`.
`TestAnalyzeDelegationApprovalIndependence` proves this directly —
familiar delegation with denied approval still blocks (via the
approval rule, on its own terms); novel delegation with approved
approval still satisfies the approval requirement (on its own terms),
even though `delegation_deviation` fires. The two remain orthogonal
by construction, not by coincidence.

## `Actor.IdentityConfidence` stays independent too

`delegation_deviation`'s `Value` is computed with no reference to
`Actor.IdentityConfidence`, and `trust.Compute` combines them the
identical way it already combines every anomaly signal with identity
confidence — as separate multiplicative terms, never one gating the
other. Identity confidence answers "how confidently do we know who
this actor is"; delegation deviation answers "how unusual is this
actor's claimed delegator" — coupling them was considered and
rejected: no concrete requirement motivates it, and doing so would
conflate two genuinely different evidence types this codebase already
keeps apart everywhere else.

## Consequences

- One new field on `VolatileFeatures`, one new bounded map on
  `Baseline`, one new opt-in signal and its weight on `anomaly.Config`.
  No new package, no new pipeline stage, no new port method, no new
  persistence technology, no new dependency.
- `store.Store`'s interface and every existing `Observe(fp, vol, now)`
  call site across the module are byte-for-byte unchanged.
- Existing `v0.1`–`v0.7` callers (including task 014's/030's own
  events, which never set `DelegatedFrom`) see byte-for-byte unchanged
  `Score`/`Decision` output and `BenchmarkEngineAnalyze`'s allocation
  profile.
- `DelegatedFrom` still carries no verified provenance after this
  task. A future task adding trusted-provenance verification is a
  distinct, larger piece of work this task deliberately does not build
  ahead of a concrete need.
