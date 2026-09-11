# Sequence Analysis

How Trustvian reasons about the *order* of an actor's behavior, not
just one event in isolation — the `v0.6` foundation
([task 025](tasks/025-sequence-analysis-foundation.md),
[ADR 0010](adr/0010-bounded-process-local-sequence-state.md)).

## The problem

Every anomaly signal through `v0.5` scores one event against its own
fingerprint's history:

```
event -> Features -> Fingerprint -> Baseline lookup -> Anomaly
```

None of them see what happened *immediately before* it. A canonical
example:

```
authenticate -> read_customer -> update_customer     (this actor's normal path)
authenticate -> read_customer -> export_customer -> delete_customer   (observed)
```

`export_customer` and `delete_customer` may each be individually
unremarkable — a `read -> export` transition might even be common
elsewhere in the fleet — but `read -> delete`, specifically, for
*this* actor, may never have happened. Order carries information the
existing signals structurally cannot see.

## What this task adds: `transition_deviation`

One new anomaly signal, integrated exactly like every other one
(`internal/anomaly.Signal`, combined via the existing noisy-OR
`combine()`, opt-in via a zero-default `Config.TransitionWeight`):

```
Anomaly.Score  (unchanged formula, one more Signal folded in)
```

`transition_deviation` answers exactly one question: **has this exact
predecessor fingerprint ever led to this destination fingerprint
before, for this actor?** Not "how often" (no rarity threshold yet),
not "how probable" (no Markov model yet) — just seen vs. never seen.
That is a deliberate scope boundary, not an oversight — see
[task 025's Non-Goals](tasks/025-sequence-analysis-foundation.md#non-goals).

## Where the state lives

There is no new `SequenceStore`. Two small additions to the existing
`internal/baseline.Baseline`/`FingerprintStats` types carry everything
this detector needs — see
[ADR 0010](adr/0010-bounded-process-local-sequence-state.md) for the
full reasoning behind reusing the existing `Baseline`/`Store`
infrastructure instead of building a parallel one:

```go
type Baseline struct {
    // ... existing fields ...
    LastFingerprintID   string    // this actor's most recent Fingerprint.ID
    LastFingerprintTime time.Time // its (ordering-guarded) timestamp
}

type FingerprintStats struct {
    // ... existing fields ...
    PredecessorCounts map[string]uint64 // bounded at 64 distinct entries
}
```

## Scope: `baseline.Key{ActorID, Environment}`

A transition is scoped to exactly the same key every other learned
signal in this codebase already uses — no new identity concept, no
mixing of unrelated actors' history. `svc-a`'s `read -> delete` and
`svc-b`'s `read -> delete` are tracked completely independently,
inherited for free from `internal/store`'s existing per-`Key` sharding.

## Sequence length: one step

The first detector operates on `E(n-1) -> E(n)` only — the smallest
possible unit of order, and a deliberately narrow foundation (see
[task 025's Non-Goals](tasks/025-sequence-analysis-foundation.md#non-goals)).
A future n-gram or Markov task can generalize `LastFingerprintID` into
a small, explicitly-bounded ring buffer without this design being
reversed — the write path (`Baseline.Observe`) and read path
(`anomaly.Score`) shapes stay the same; only how much history is kept
grows.

## Event ordering

`Baseline.Observe` only records a transition, and only advances
`LastFingerprintID`/`Time`, when the incoming event's timestamp
**strictly follows** `LastFingerprintTime` — the identical guard
`FingerprintStats.observe` already applies to interval statistics, for
the identical reason: an out-of-order or backdated event (clock skew,
replay, a deliberately backdated event from an untrusted source) must
not be allowed to (a) be scored as following a predecessor it didn't
actually follow in real time, or (b) silently overwrite what the
*next* legitimate event's transition should be measured against. An
out-of-order event still updates its own `FingerprintStats` as usual
(`Count`, etc.) — it simply contributes no transition information in
either direction.

## Cold start

Two distinct "not enough history" cases, handled differently, matching
this codebase's existing cold-start philosophy (see
[ARCHITECTURE.md § Cold start](ARCHITECTURE.md#cold-start-two-numbers-not-one)):

- **No predecessor at all** (`LastFingerprintID == ""` — the actor's
  first-ever observation): `transition_deviation` does not fire at
  all. There is no transition to evaluate, so reporting one would be
  fabricating evidence, not observing it.
- **A predecessor exists, but this exact transition has never been
  seen**: `transition_deviation` fires at maximal value (`1`) — the
  same "genuinely novel scores as novel" philosophy
  `categorical_novelty` already applies to a never-before-seen
  fingerprint. This is **not** automatically treated as a critical
  attack: `TransitionWeight` defaults to `0`, so the signal is
  computed and reported (visible in `Anomaly.Contributors` for
  explainability) but contributes nothing to `Score` until an operator
  opts in — the identical mechanism `FrequencyWeight`/
  `TimePatternWeight` already use, and for the identical reason: a
  brand-new signal needs real traffic to calibrate against before its
  raw "novel" reading is a trustworthy basis for a decision.

## Resource bounds

`PredecessorCounts` is capped at 64 distinct predecessor entries per
destination fingerprint (`maxPredecessors` in `internal/baseline`).
Once at the bound, a genuinely new predecessor is not added; existing
entries keep accumulating normally — a deliberately simple "first 64
distinct predecessors win" policy, not LRU. See
[ADR 0010](adr/0010-bounded-process-local-sequence-state.md#why-bounded)
for the full reasoning and
[SECURITY.md § Sequence state](SECURITY.md#sequence-state) for the
threat model this closes.

## Activation

Purely a Go SDK option — no new activation mechanism:

```go
cfg := anomaly.DefaultConfig()
cfg.TransitionWeight = 0.7 // opt-in: defaults to 0

engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(cfg))
```

There is no CLI flag or Collector config field for this yet (see
[task 025's Non-Goals](tasks/025-sequence-analysis-foundation.md#non-goals))
— a single-event CLI invocation has no predecessor to evaluate a
transition against in the first place, and wiring it into the
Collector processor is separate, later work once a real consumer needs
it.

## What's next

`docs/ROADMAP.md` § v0.6 lists the planned progression beyond this
foundation (transition/rarity scoring, n-gram detection, Markov
transition probabilities) — none of it is implemented yet. This
document describes what exists today, not what's planned; it will be
updated as each slice actually ships, not before.
