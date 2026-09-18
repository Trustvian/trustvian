# 0019 — Bounded fingerprint admission: refuse, never evict

**Status:** Accepted

## Context

`Baseline.Fingerprints` was the one behavioral state dimension with no
outer bound. Every map *inside* a `FingerprintStats` entry was capped at
64 — `PredecessorCounts` ([ADR 0011](0011-transition-rarity-statistic-and-orientation.md)),
`TrigramCounts` ([ADR 0012](0012-bounded-trigram-behavioral-context.md)),
`DelegatorCounts` ([ADR 0016](0016-delegation-as-behavioral-evidence-not-provenance.md)) —
each with the same reasoning: the key is derived from `Event` fields a
caller supplies, so an untrusted source could otherwise grow it without
limit.

That reasoning applies with equal force to the outer map, and was not
applied to it. `Fingerprint.ID` is a hash of stable features the caller
controls, so one actor emitting a distinct operation name per call —
trivially arranged by anything that puts an identifier in a route — grew
its baseline without limit, and through `internal/store/postgres`'s
one-`jsonb`-row-per-actor layout, its database row with it.

The v1.0 production-readiness audit also found the documentation
claiming the opposite. `docs/observability.md` published a memory table
derived from "a baseline driven past every cardinality cap measures
~13 KB", a figure that comes from `TestLargeBoundedBaselineRoundTrips`
driving **120** fingerprints. With no cap on the outer map, that table
described a bound the code did not enforce — the worse half of the
problem, because an operator provisions from it.

The existing test named the gap honestly rather than hiding it:
`TestObserveUnboundedFingerprintsDoesNotPanic` drove 5,000 distinct
fingerprints and asserted only that nothing panicked, with a comment
stating it did not bound memory growth.

## Decision

`internal/baseline` gains a fourth bound, `maxFingerprints = 512`,
enforced in `Baseline.Observe` as **admission control**:

- A fingerprint the baseline already knows always keeps learning,
  however full the baseline is.
- An unknown fingerprint is learned only while the baseline holds fewer
  than `maxFingerprints` identities.
- At capacity, an unknown fingerprint is **refused, not swapped in**.
  The observation is not an error, does not stop the pipeline, and does
  not evict anything.

Refusal also leaves the sequence history window where it is: a
fingerprint that was not learned does not become the predecessor a later
observation records a transition from. Without that, the predecessor's
half of the transition would create a `FingerprintStats` entry for an
identity that was never admitted — the one other path into the map, now
guarded explicitly.

The bound is a fixed internal constant. It is not exposed through the
public API, `config`, the CLI, an environment variable, or the Collector
processor's configuration.

A baseline that already holds more than 512 identities — learned before
this bound existed — keeps every one of them and keeps updating them.
It admits nothing new. It is not truncated on load, on restore, on
migration, or at any other point.

### Why 512

From the one size measurement this repository has: a baseline holding
120 fingerprints with every inner cap filled serializes to roughly
13 KB, so about 110 bytes of serialized state per fingerprint. 512
bounds a baseline at roughly 50–60 KB serialized — large enough that a
service with a genuinely wide route surface is learned in full, small
enough that a large actor population stays predictable.

It is deliberately far larger than the 64 used for the inner maps.
Those bound an actor's repertoire *relative to one other action*; this
bounds its entire distinct behavior set, and a real API service has far
more distinct routes than it has distinct predecessors for any single
one of them.

### Why refuse rather than evict

This is the load-bearing half of the decision, and it follows from how
an absent fingerprint scores.

`anomaly.Score` reads `bl.Fingerprints[fp.ID]`; when the fingerprint is
absent, `familiarity` is 0, so `categorical_novelty` is maximal **and**
`Confidence` is 0. `trust.Compute` combines them as
`effectiveAnomaly = Anomaly.Score × Anomaly.Confidence`. At confidence
zero, a maximally novel event contributes *nothing* to the trust
penalty — the cold-start mechanism [ADR 0001](0001-hexagonal-core-and-pipeline-shape.md)'s
pipeline relies on, working as designed.

Eviction therefore does not merely cost accuracy. An attacker able to
influence fingerprints could flood an actor's baseline with
manufactured identities, evict the ones representing its genuine
behavior, and return that actor to cold start — where anomaly
contributes nothing to trust at all. That converts a
resource-exhaustion problem into a **detection-suppression** one, which
is strictly worse than the bug being fixed, and contradicts the
learning-gate reasoning in
[ADR 0014](0014-ai-agents-as-first-class-behavioral-actors.md) that an
attacker must not be able to wear the system down by repetition.

Refusal has a cost too, stated rather than hidden: an actor whose
legitimate behavior set exceeds 512 identities stops learning new ones,
and an attacker who fills capacity first denies learning of behavior
that arrives later. But everything already learned keeps working, which
is the property that matters for a security decision — and learning is
already gated, so only `ALLOW`/`OBSERVE_ONLY`/`ALERT` traffic can
consume capacity at all.

## Alternatives considered

- **LRU / LFU / oldest-first eviction.** Rejected for the
  detection-suppression reason above. Each also needs per-entry
  recency or frequency metadata maintained on the hot path, for a
  mechanism whose purpose is to discard learned security state.
- **Two-tier: protect mature fingerprints, evict immature ones.** The
  strongest alternative — it keeps adapting while protecting
  established detection. Rejected as disproportionate for this task:
  it introduces genuinely new behavioral semantics, and the mature tier
  itself still needs a cap, so it re-poses the same question one level
  down. Reconsider if refusal proves too blunt in practice.
- **Make the cap configurable.** Rejected here on purpose. A
  resource/security invariant is safer as a fixed bound than as a knob
  an operator can set to something unbounded, and the public
  configuration surface is being reviewed as its own v1.0 work rather
  than extended in passing.
- **Truncate oversized legacy baselines on load.** Rejected: it deletes
  learned detection state during an upgrade, silently, at exactly the
  moment an operator is least able to notice. Grandfathering costs
  nothing — those baselines cannot grow further.

## Consequences

- Per-actor behavioral state is structurally bounded for the first
  time, so documented per-actor figures describe something the code
  enforces.
- An actor with more than 512 distinct legitimate fingerprints will not
  learn beyond the cap. Nothing surfaces that today; whether it needs a
  signal is a real follow-up question, not a settled one.
- Baselines learned before this bound keep their full cardinality
  indefinitely. The invariant is therefore "≤ 512 for anything learned
  under this implementation", not "≤ 512 everywhere" — deliberately
  not described as convergence, since nothing converges.
- `Observe`'s copy-on-write cost is now bounded by 512 entries rather
  than unbounded. Whether O(512) per observation is acceptable is a
  measurement question, tracked separately.
- No storage schema change, no persisted-representation change, and no
  public API, configuration, or CLI change.
