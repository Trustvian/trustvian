# 0012 — Bounded higher-order behavioral context: 3-grams, orientation, and why arbitrary n waits

## Context

[Task 025](../tasks/025-sequence-analysis-foundation.md) gave Trustvian
one step of order (`A -> B`, binary seen/unseen). [Task 026](../tasks/026-transition-rarity.md)
graded that into common/uncommon/rare. Both still only ever compare
one event against the single fingerprint that immediately preceded it
— they cannot see the canonical gap this task closes: `A -> B` and
`B -> C` can each be completely familiar on their own while the
specific sequence `A -> B -> C` has never happened. [Task 027](../tasks/027-bounded-ngram-detection.md)
adds exactly that: a fixed, bounded 3-gram detector, built the same
way tasks 025/026 were — extending `internal/baseline`'s existing
state model, not a parallel one.

## Why 3-grams first, not arbitrary n

The task's own brief is explicit that `n = 2..100` configurability is
speculative generality this task must not build. A fixed 3-gram is
smaller state (one more fingerprint of history than task 025/026
already carry), easier to reason about and benchmark in isolation, and
already demonstrates the genuinely new capability (higher-order
novelty invisible to pairwise signals) this milestone needs to prove
before any further generalization is justified. Nothing in this
design forecloses a future n-gram generalization — see "Future
extension" below — but building it now, with no consumer need driving
the specific value of `n`, would be exactly the premature abstraction
CLAUDE.md and this milestone's own non-goals warn against.

## Minimal history state

A 3-gram needs exactly one more fingerprint of history than a
transition does. `Baseline` gains one new field,
`PreviousFingerprintID string` — the fingerprint two steps back —
alongside the existing `LastFingerprintID`/`LastFingerprintTime` (one
step back). No `PreviousFingerprintTime` field exists: unlike
`LastFingerprintID` (re-validated against the *current* observation's
timestamp on every read), `PreviousFingerprintID`'s ordering validity
is established transitively, once, at the moment it is assigned — it
only ever takes a value that was itself a validly-ordered
`LastFingerprintID` at some earlier point (see `Baseline.Observe`'s
`advances` guard), so a second timestamp field to re-check that would
be redundant state, not a safety requirement. No raw `Event` history,
no ring buffer, no arbitrary-length window — precisely the "minimal
additional state" the task's own brief asks for.

## The orientation problem, generalized one level up

The same question [ADR 0011](0011-transition-rarity-statistic-and-orientation.md)
had to answer for 2-grams recurs here, one level up: given a 3-gram
`A -> B -> C`, the useful question is `P(C | A, B)` — "given what this
actor just did (A, then B), how unusual is C next" — not
`P(A, B | C)`, which a naively-oriented destination-keyed map would
give if misread.

**Numerator:** `TrigramCounts` — a new, bounded map on the
*destination* `FingerprintStats` (`C`'s own entry), keyed by a
`TrigramKey{First, Second}` pair (`First` = the grandparent, `Second`
= the immediate predecessor) — directly generalizes `PredecessorCounts`
(task 025) from a single-fingerprint key to a two-fingerprint key. The
destination `C` itself stays implicit (whichever `FingerprintStats`
entry holds the map), exactly as it already is for `PredecessorCounts`.

**Denominator:** the harder question, and the reason this task needed
its own ADR rather than reusing task 026's `OutgoingTransitionTotal`
outright. `OutgoingTransitionTotal` is a *scalar* on a single
fingerprint's own stats — correct for "how many transitions has this
one fingerprint originated," but a 3-gram's denominator is "how many
continuations has this specific *pair* `(A, B)` originated," and a
single fingerprint `B` can participate in many distinct pairs (one per
distinct grandparent `A`). A scalar on `B`'s stats would silently
answer the wrong question — the identical mistake ADR 0011's whole
design exists to avoid, recurring one level up if not caught. The
resolution: `TrigramContinuationTotal map[string]uint64`, a new
*bounded map* (not a scalar) on the immediate predecessor `B`'s own
stats, keyed by grandparent `A` — giving

```
frequency(A, B -> C) = TrigramCounts_C[{A,B}] / TrigramContinuationTotal_B[A]
```

as two O(1) map reads, no scan of `Baseline.Fingerprints` required,
exactly the performance bar ADR 0011 set for the 2-gram case.

## Why `TrigramContinuationTotal` needed its own bound, not an inherited one

A real correctness/security subtlety this task's own review caught:
it would be tempting to assume `TrigramContinuationTotal_B`'s key set
is bounded "for free" because it is a subset of the grandparents that
have ever led into `B`, which `PredecessorCounts_B`'s own
`maxPredecessors` cap already bounds. This is **not** true.
`Baseline.Observe`'s history-window shift (`PreviousFingerprintID`
advancing) happens unconditionally on every valid advance, regardless
of whether `recordPredecessor` actually admitted the new key into
`PredecessorCounts_B` or rejected it for being past that map's own
bound — so a grandparent can become `PreviousFingerprintID`, and later
drive a `TrigramContinuationTotal_B`/`TrigramCounts_C` write, even on a
call where `PredecessorCounts_B` itself never recorded it. Both new
maps therefore carry their own explicit, independently-enforced bound,
`maxTrigramPredecessors` (64, matching `maxPredecessors`'s own value
for the identical "generous for real traffic, trivial worst-case
memory" reasoning, not a coincidence) — see
`internal/baseline.maxTrigramPredecessors`'s own doc comment for the
full argument.

## Why a struct key, not a delimited string

`TrigramKey{First, Second string}` is a plain, comparable struct —
constructing and comparing one on the anomaly-scoring hot path
allocates nothing, where a concatenated string key (`A + "|" + B`)
would allocate a new string on every single lookup, directly
conflicting with this task's own O(1)-hot-path requirement. The one
place a delimited string does appear is `TrigramKey.MarshalText`/
`UnmarshalText` — needed purely because `encoding/json` (which
`store.FileStore` uses to persist every `Baseline`) requires a map's
key type to be a string/integer kind or implement
`encoding.TextMarshaler`; a bare struct does not qualify on its own.
`"|"` is a safe separator specifically because `Fingerprint.ID` is
always a lowercase hex string (`strconv.FormatUint(hash, 16)` — see
`internal/fingerprint.Compute`), which cannot contain it. This
encoding is a persistence-format detail, verified end-to-end (not just
in isolation) by `TestFileStoreSurvivesRestartWithTrigramState`
(`internal/store/file_test.go`) — the one genuinely new persistence
risk this task introduces, since every prior map this module has ever
persisted used a plain string key.

## Naming and cold start

Following ADR 0011's own precedent exactly: the signals are
`ngram_deviation` (binary seen/unseen, mirroring `transition_deviation`)
and `ngram_rarity` (graded, mirroring `transition_rarity`), never
described as a "probability." `ngram_rarity` is gated on
`TrigramContinuationTotal_B[A] >= Config.MinNGramObservations`
(default 20, matching `MinObservations`/`MinTransitionObservations`'s
own precedent) — a separate config field from
`MinTransitionObservations`, deliberately: the two gate statistically
different denominators (total outgoing transitions from a single
fingerprint, vs. total 3-gram continuations from a specific
two-fingerprint pair) that happen to share a default value for the
same underlying reason, not because they mean the same thing. Cold
start has three distinct "not enough history" states, not two: no
predecessor at all (first-ever observation), a predecessor but no
grandparent (second-ever observation — enough for a 2-gram, not a
3-gram), and both present (third-ever observation onward — a complete
3-gram exists to evaluate). `ngram_deviation`/`ngram_rarity` are
evaluated only in the third case, nested inside the existing
`bl.LastFingerprintID != "" && ...` gate task 025 already established.

## Preserving the unseen/rare distinction, and interaction with pairwise signals

`ngram_deviation` and `ngram_rarity` are mutually exclusive by
construction, identically to `transition_deviation`/`transition_rarity`
— `ngram_deviation` fires exactly when `TrigramCounts_C[{A,B}] == 0`;
`ngram_rarity` is only evaluated when that count is `> 0`.

A 3-gram signal and its corresponding pairwise signal *can* fire
together on the same event — whenever the final hop `B -> C` is itself
unseen, `A, B -> C` is trivially also unseen (a 3-gram cannot be
familiar if its last transition never happened at all), so
`transition_deviation` and `ngram_deviation` fire simultaneously.
`combine()`'s noisy-OR is not redesigned for this (per the task's own
explicit instruction) — every contribution is already clamped to
`[0,1]` before multiplying, so the combined `Score` stays bounded and
finite regardless of how many sequence signals fire together or how
correlated they are, proven by
`TestScoreCombinedSequenceSignalsRemainBounded`. This is the same
compounding property every other multi-signal noisy-OR combination in
this package already has (e.g. `categorical_novelty` +
`sensitive_target` + `error_deviation` co-firing today); task 027 does
not introduce a new failure mode, only a new pair of signals that can
participate in it. Both new weights default to `0`, so no existing or
new caller sees any score change until an operator opts in.

## Future extension

`TrigramCounts`/`TrigramContinuationTotal` are exactly the sufficient
statistics a future n-gram generalization (n > 3) would need more of,
not different statistics: extending to a 4-gram would add one more
history field (`PrePreviousFingerprintID`, say) and widen `TrigramKey`
to a 3-field key, following the identical orientation and bounding
argument this ADR already made — it would not require revisiting this
ADR's decisions, only repeating its pattern one level further.
Similarly, a future Markov task can build on these same counters
directly (see ADR 0011's own "Why Markov still waits," which applies
identically here — no transition matrix, no smoothing, no higher-order
probability model exists yet).

## Consequences

- One new scalar field, `Baseline.PreviousFingerprintID string`.
- Two new bounded maps on `FingerprintStats`: `TrigramCounts
  map[TrigramKey]uint64` (on the destination) and
  `TrigramContinuationTotal map[string]uint64` (on the immediate
  predecessor, keyed by grandparent) — each independently bounded at
  `maxTrigramPredecessors` (64), not inheriting `PredecessorCounts`'s
  bound.
- One new public (within `internal/baseline`) comparable type,
  `TrigramKey`, with `MarshalText`/`UnmarshalText` solely for
  `encoding/json` map-key compatibility.
- `anomaly.Config` gains `NGramWeight`, `MinNGramObservations` (default
  20), `NGramRarityWeight` (default 0) — all following the existing
  "ships opt-in" precedent.
- No saturating-arithmetic special case for either new map's counters
  — matching `PredecessorCounts`/`OutgoingTransitionTotal`'s own
  unguarded-increment precedent.
- Existing `v0.5`/task-025/task-026 callers see byte-for-byte unchanged
  `Score` output: both new weights default to `0`, and
  `transition_deviation`/`transition_rarity`'s own behavior is
  completely untouched.
