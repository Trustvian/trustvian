# 0011 — Transition rarity: the statistic, its orientation, and why Markov waits

## Context

[Task 025](../tasks/025-sequence-analysis-foundation.md) shipped a
binary `transition_deviation` signal: has this exact predecessor
`Fingerprint.ID` ever led to this destination before, for this actor?
[Task 026](../tasks/026-transition-rarity.md) evolves that into a
graded measure — common / uncommon / rare, not just seen / unseen —
without building a Markov model. Before writing any code, this task
had to answer a question that is easy to get silently wrong: **which
conditional probability is `FingerprintStats.PredecessorCounts`
actually shaped to estimate?**

## The orientation problem

`PredecessorCounts` lives on the *destination* fingerprint's own
`FingerprintStats`:

```go
bl.Fingerprints[B].PredecessorCounts[A] // count of A -> B transitions
```

Enumerating one destination's `PredecessorCounts` map answers "of the
times B was reached, how often did each predecessor lead here" — that
is `P(predecessor | destination)`, i.e. `P(A | B)`.

That is **not** the question a behavioral-transition detector needs to
ask. The question is "given what this actor just did (A), how unusual
is doing B next" — `P(destination | predecessor)`, i.e. `P(B | A)`.
These are genuinely different statistics, and `P(A|B)` from an
unrelated destination's map would be a silently wrong answer if used
as if it were `P(B|A)` — the two only coincide when every predecessor
is equally likely overall, which is not a property this data has.

`P(B|A) = count(A -> B) / (total transitions where A was the
predecessor, to any destination)`. The numerator
(`bl.Fingerprints[B].PredecessorCounts[A]`) already exists, from task
025. The denominator — "total outgoing transitions from A" — does
**not**: computing it from the existing state would require summing
`PredecessorCounts[A]` across *every* `FingerprintStats` entry in
`Baseline.Fingerprints`, an O(number of known fingerprints) scan on
every single `Analyze` call. That fails this task's own explicit
performance requirement (near-O(1) per lookup, no full-map scans on
the hot path) outright.

## Decision

**Add exactly one new scalar field**,
`FingerprintStats.OutgoingTransitionTotal uint64` — the number of
valid transitions where *this* fingerprint was the predecessor, to any
destination. It lives on the *predecessor's* own `FingerprintStats`
entry (symmetric to `PredecessorCounts` living on the destination's),
incremented once per valid transition, alongside the existing
`PredecessorCounts` update, in `Baseline.Observe`.

```
frequency(A -> B) = PredecessorCounts_B[A] / OutgoingTransitionTotal_A
```

This is a genuine empirical relative frequency (a maximum-likelihood
estimate of `P(B|A)`), computed with exactly two O(1) map/field reads:
`bl.Fingerprints[B].PredecessorCounts[A]` (already available) and
`bl.Fingerprints[A].OutgoingTransitionTotal` (the one new field). No
scan of `Baseline.Fingerprints` is required at any point.

**Naming is deliberately conservative.** The anomaly signal this
produces is `transition_rarity`, and the underlying number is called a
*frequency* or *rarity*, never a *probability* or *Markov transition
probability* — it is an unsmoothed empirical ratio (no Laplace
smoothing, no stationary distribution, no chain), and calling it a
probability model would overstate what it actually is. See [task
026's Non-Goals](../tasks/026-transition-rarity.md#non-goals) for the
explicit line between this and the deferred Markov task.

## Minimum support

A frequency computed from a tiny sample is not evidence of rarity, it
is evidence of *not enough data* — the identical cold-start problem
`categorical_novelty`'s own `Confidence`/familiarity ramp already
exists to separate from genuine anomaly. `transition_rarity` is gated
on `OutgoingTransitionTotal_A >= anomaly.Config.MinTransitionObservations`
(default `20`, matching `MinObservations`'s own existing default and
its own justification: with 20 samples, the smallest nonzero frequency
observable is `1/20 = 5%`, a reasonable minimum resolution). Below the
gate, `transition_rarity` does not fire at all — not "fires weakly,"
not "fires with reduced confidence" — the identical "insufficient
history, not evidence" stance `frequency_deviation`'s own
`IntervalObservations == 0` gate already takes.

## Mapping frequency to a bounded signal value

`transition_rarity`'s raw `Value` is `1 - frequency`, applied **only**
after the minimum-support gate above has already passed. No sqrt/log
transform, no threshold bands, no additional smoothing:

- **Why not a nonlinear transform.** A transform such as `sqrt(1 -
  frequency)` or a log-odds mapping would make the signal harder to
  explain in a `Detail` string ("this transition happens 0.4% of the
  time" is legible; "this transition has a log-odds-transformed rarity
  of 0.87" is not) for no corresponding calibration benefit this task
  can actually justify with data. [Task 026](../tasks/026-transition-rarity.md#no-arbitrary-magic-weights)'s
  own brief is explicit: no opaque formulas, no magic weights without
  justification.
- **Why linear is not "naively" `1 - frequency` despite looking
  identical.** The specific failure mode the brief warns about —
  "`frequency = 0.01` does not necessarily mean `anomaly = 0.99`" — is
  precisely the *tiny-sample* problem the minimum-support gate above
  already closes. Once `OutgoingTransitionTotal_A >=
  MinTransitionObservations`, a frequency of `0.01` is a genuinely
  meaningful statement ("this specific next-action happens about 1%
  of the time for this actor, out of at least 20 observed outgoing
  transitions"), and `1 - frequency` is a faithful, transparent
  reading of it, not an overreaction to noise.
- **Bounds.** `frequency` is always in `[0, 1]` by construction (a
  count divided by a total that includes it), so `Value = 1 -
  frequency` is always in `[0, 1]` too — no clamping, no `NaN`/`Inf`
  possible as long as the denominator gate (`>= MinTransitionObservations`,
  hence `> 0`) has already passed, which it always has before this
  formula ever runs.

## Preserving the unseen/rare distinction

`transition_deviation` (task 025, unchanged) and `transition_rarity`
(this task) are **mutually exclusive by construction**, not merely by
convention: `transition_deviation` fires exactly when
`PredecessorCounts_B[A] == 0`; `transition_rarity` is only evaluated
when `PredecessorCounts_B[A] > 0`. A transition is either never seen
(`transition_deviation`) or seen-but-possibly-rare
(`transition_rarity`) — never both, and the distinction the task's own
brief asks to preserve ("an unseen transition should remain
identifiable separately from a rare but previously observed
transition") is enforced structurally, not by a naming convention that
could drift.

## Why Markov still waits

This ADR's `frequency(A -> B)` *is*, mathematically, a first-order
Markov transition probability estimate. It is deliberately not called
one, and this task deliberately does not build the machinery a real
Markov treatment would need beyond it:

- **No transition matrix / stationary distribution.** Nothing here
  computes or exposes a full `P(* | A)` row — only the one
  `P(B | A)` this call's actual predecessor/destination pair needs.
  Building the full row would need to enumerate every destination
  `PredecessorCounts_D[A]` could appear in — the same O(N) scan this
  ADR's whole design exists to avoid on the hot path.
- **No higher-order history.** `A` is a single predecessor (task 025's
  own one-step scope, unchanged) — not a state derived from the last N
  actions.
- **No smoothing/priors.** A real Markov-probability treatment usually
  wants Laplace/Bayesian smoothing so an unseen transition doesn't get
  an estimate of exactly `0`; this task doesn't need that, because the
  unseen case is handled entirely separately, by
  `transition_deviation`, per the section above.

A future Markov task can build directly on `OutgoingTransitionTotal`
and `PredecessorCounts` — both are already exactly the sufficient
statistics a first-order Markov model needs — without this ADR's
decisions needing to be revisited; it would add the missing pieces
above (full-row queries, smoothing, or an explicit probability-model
label), not change the counters underneath them.

**Update (task 028):** [task 028](../tasks/028-markov-transition-scoring.md)
revisited this section and confirmed every point above still holds — no
transition matrix, no higher-order history, no smoothing was added.
What it *did* add, `markov_surprisal`, is not "the future Markov task"
this section anticipated: it is an alternative bounded severity curve
over this ADR's own `frequency(A -> B)`, mathematically proven to carry
zero additional ranking information beyond `transition_rarity` (see
[ADR 0013](0013-first-order-markov-surprisal-without-duplicate-evidence.md)).
The genuinely open items named in this section — a full `P(*|A)` row,
higher-order history, smoothing — remain unbuilt and undecided.

## Consequences

- One new scalar field, `FingerprintStats.OutgoingTransitionTotal
  uint64`, incremented alongside `PredecessorCounts` in
  `Baseline.Observe`. No new map, no new cardinality dimension — the
  existing `maxPredecessors` bound (ADR 0010) is unaffected and
  remains sufficient.
- No saturating-arithmetic special case: `OutgoingTransitionTotal++`
  is a plain increment, matching `FingerprintStats.Count`'s own
  existing, unguarded `s.Count++` — introducing saturation logic for
  this one counter while every other `uint64` counter in this struct
  lacks it would be an inconsistent, unjustified special case, not a
  genuine safety improvement (reaching `2^64` through legitimate
  per-event increments is not a realistic concern for any deployment's
  actual lifetime).
- `anomaly.Config` gains `TransitionRarityWeight float64` (defaults to
  `0`, opt-in — the same precedent every prior signal weight in this
  package established) and `MinTransitionObservations uint64`
  (defaults to `20`).
- Existing `v0.5`/task-025 callers see byte-for-byte unchanged `Score`
  output: `TransitionRarityWeight` defaults to `0`, and
  `transition_deviation`'s own behavior is completely untouched by
  this task.
