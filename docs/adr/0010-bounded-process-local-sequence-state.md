# 0010 — Bounded, process-local sequence state, embedded in `Baseline`

## Context

[Task 025](../tasks/025-sequence-analysis-foundation.md) (`v0.6`,
Behavioral Detection Depth) needed to answer, before writing any code,
where "what did this actor do immediately before this event" should
live, and how far that state needs to be generalized on day one.

The obvious-looking answer — a new `SequenceKey`/`SequenceStore`
abstraction, a `SequenceAnalyzer` interface, a bounded ring buffer of
recent events per actor — was evaluated and rejected in favor of a much
smaller change, for reasons this ADR records so a future task
generalizing this (n-gram, Markov — see docs/ROADMAP.md § v0.6) doesn't
have to re-derive them.

## Decision

**There is no new `SequenceStore`, `SequenceKey`, or `SequenceAnalyzer`
type.** The minimum state a single-step (`E(n-1) -> E(n)`) transition
detector needs — "what was the last thing this actor did, and when" —
is two new fields on the existing `internal/baseline.Baseline`:

```go
type Baseline struct {
    // ... existing fields unchanged ...
    LastFingerprintID   string
    LastFingerprintTime time.Time
}
```

and one new, bounded field on the existing `FingerprintStats` (the
per-destination-fingerprint record `Baseline.Fingerprints` already
holds one of, per actor):

```go
type FingerprintStats struct {
    // ... existing fields unchanged ...
    PredecessorCounts map[string]uint64 // bounded at 64 distinct entries
}
```

This reuses, unmodified:

- **Scope/identity** — `baseline.Key{ActorID, Environment}` is already
  exactly the "smallest stable scope" a transition should be measured
  within — the same scope every other per-actor learned signal in this
  codebase already uses; no new identity concept exists.
- **Concurrency safety** — `internal/store`'s existing per-Key
  sharded-lock `Store` (`InMemory`, `FileStore`) already owns
  serializing access to a `Baseline` value; the two new fields ride
  along inside the same value, under the same lock, for free.
- **Persistence** — `FileStore`'s existing `encoding/json` round-trip
  of `baseline.Baseline` picks up the new fields automatically (no
  explicit tag needed, matching every other field in the struct); an
  old file missing them unmarshals to the correct, safe zero value
  (`""`, zero `time.Time`, `nil` map) — cold start, not an error state,
  exactly like `HourActivity`'s own precedent from
  [task 017](../tasks/017-baseline-time-patterns.md).
- **Immutability discipline** — `Baseline.Observe`'s existing
  copy-on-write contract extends unchanged: the new fields are copied
  by value into each returned `Baseline`, and `PredecessorCounts`
  itself is never mutated in place (see `recordPredecessor`) — a real
  bug caught and fixed during this task's own review, not merely
  asserted (see `TestBaselineObservePredecessorCountsIsImmutable`).
- **Ordering/out-of-order handling** — the same "only a strictly
  forward-moving timestamp carries information" guard
  `FingerprintStats.observe`'s interval statistics already use.
- **Signal reporting** — the new evidence surfaces as one more
  `anomaly.Signal` (`transition_deviation`), combined via the existing
  noisy-OR `combine()`, opt-in via a zero-default `Config.TransitionWeight`
  — the identical mechanism `FrequencyWeight`/`TimePatternWeight`
  already established. No new `Result` field, no second scoring path.

## Why process-local, not distributed

`internal/store.Store` already documents that `InMemory` does not
survive a process restart and that persistence is a per-implementation
concern (`FileStore`). This task's sequence state inherits exactly that
posture — it introduces no new claim about cross-instance consistency,
and no new persistence mechanism. If Trustvian eventually runs as
multiple cooperating instances, that is a `Store` implementation
concern (a shared backing store), not something `Baseline`'s own shape
needs to anticipate today. Adding a distributed coordination
dependency (Redis, etc.) now, with no such consumer, would be exactly
the speculative infrastructure CLAUDE.md's "avoid premature
microservices" instruction warns against.

## Why bounded

`PredecessorCounts` is the one genuinely new *unbounded-growth-shaped*
structure this task introduces (`Baseline.Fingerprints` itself was
already unbounded before this task, and is out of this ADR's scope to
revisit). Because `Fingerprint.ID` is derived from caller-controlled
`Event` fields, an actor who can vary the *previous* action on every
call could otherwise grow one destination fingerprint's
`PredecessorCounts` without bound. `maxPredecessors = 64` bounds this:
generous for real traffic (an actor's distinct predecessor actions are
its own behavioral repertoire, not per-event entropy — typically a
handful to a few dozen in practice), trivial in absolute memory even at
the cap.

The eviction policy at the bound is deliberately the simplest one that
is still safe, not LRU: once at 64 distinct predecessors, a genuinely
new one is not added; existing entries keep accumulating normally (see
`recordPredecessor`). The failure mode this produces — a
not-yet-tracked predecessor always reads as "unseen" — is the
conservative, safe direction: it never causes an already-legitimate,
tracked transition to be silently forgotten, and it never contributes
false confidence that a transition is familiar when it might not
genuinely be tracked. An LRU policy was considered and rejected for
this first slice as unneeded complexity — nothing about the "first
detector should be simple" mandate (task 025's own brief) is served by
it, and it can be introduced later, additively, if real traffic ever
demonstrates the simpler policy is insufficient.

## Why no TTL / active eviction

`FingerprintStats.IsStale` already establishes this codebase's
existing philosophy for state that hasn't been touched in a while:
**staleness is reported, not acted on** — "this package only reports
staleness; it does not act on it... deciding what to do with a stale
entry, if anything, is a caller's policy decision." `LastFingerprintID`/
`LastFingerprintTime` follow the identical posture: nothing
automatically expires them. A future task can build an active
TTL-based sweep on top of this if a concrete deployment need
(long-running processes accumulating many never-returning actors)
demonstrates it, but this task does not invent that infrastructure
speculatively — see task 025's own Non-Goals.

## Alternatives considered

- **A separate `SequenceStore` / `SequenceKey` abstraction**, as this
  task's own illustrative brief sketched. Rejected: `baseline.Key`
  already *is* the correct scope, and `internal/store.Store` already
  *is* the correct concurrency/persistence boundary — a second,
  parallel port next to the existing one would duplicate
  infrastructure this task's evidence genuinely needs from `Baseline`
  anyway (the destination fingerprint's own maturity, for one). Task
  025's own brief explicitly permits this outcome ("use existing
  domain types where possible... do not expose a new public type
  unless necessary").
- **A `SequenceAnalyzer` interface**, for a pluggable set of future
  detectors. Rejected for the same reason ADR 0001 gives for not
  building an anomaly-algorithm plugin interface: there is exactly one
  deterministic detector today (`transition_deviation`); an interface
  earns its keep when a second, genuinely different implementation
  exists, not before.
- **A bounded ring buffer of the last N events**, generalizing
  immediately to n-gram-shaped history. Rejected for *this* task:
  section 24/37 of the brief are explicit that a single-step
  `E(n-1) -> E(n)` transition is the correct scope for the foundation,
  and a length-1 "last fingerprint" is sufficient for that — a future
  n-gram task can introduce the ring buffer additively once it actually
  needs more than one step of history, without this ADR's decisions
  needing to be reversed.

## Consequences

- Zero new public API. `internal/baseline`, `internal/anomaly`, and
  `internal/store` gain new *internal* fields/behavior only;
  `trustvian.Result`, `config.PolicyConfig`/`AlertConfig`, the CLI, and
  `processor/` are all untouched.
- Existing `v0.5` callers see byte-for-byte unchanged `Score` output:
  `Config.TransitionWeight` defaults to `0`
  (`TestDefaultConfigTransitionWeightIsOptIn`), and
  `BenchmarkEngineAnalyze`'s allocation profile is unchanged (456 B/op,
  17 allocs/op, same as before this task — see
  [PERFORMANCE.md](../PERFORMANCE.md)).
- A future n-gram/Markov task generalizing beyond a single predecessor
  will need to replace `LastFingerprintID string` with a small, still
  explicitly-bounded ring buffer — a strictly additive change to
  `Baseline`'s shape, not a redesign of where sequence state lives or
  how it's synchronized.
