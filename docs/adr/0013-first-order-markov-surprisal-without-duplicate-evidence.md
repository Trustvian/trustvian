# 0013 — First-order Markov surprisal without duplicate evidence

## Context

[Task 028](../tasks/028-markov-transition-scoring.md) asks Trustvian to
add "Markov transition scoring." Before writing any code, this task
requires answering a specific, mandatory question: **what capability
would Markov scoring provide that task 026's `transition_rarity`
does not already provide?**

## The duplication analysis (mandatory, done first)

[Task 026](../tasks/026-transition-rarity.md) already computes:

```
frequency(A -> B) = PredecessorCounts_B[A] / OutgoingTransitionTotal_A
transition_rarity = 1 - frequency
```

which is already an empirical estimate of `P(B|A)`, reported as a
value that decreases monotonically as `P(B|A)` increases. The
textbook first-order Markov "surprisal" statistic is

```
surprisal(A -> B) = -log(P(B|A))
```

**These are not two different pieces of evidence — they are two
different curves applied to the exact same underlying number,
`frequency = count/total`.** `rarity = 1 - frequency` and
`surprisal = -log(frequency)` are both strictly monotonically
decreasing functions of `frequency` on `(0, 1]`. Formally: there
exists a strictly increasing function `g` such that
`surprisal = g(rarity)` (specifically, since `frequency = 1 - rarity`,
`g(r) = -log(1 - r)`). Any bounded normalization of surprisal (e.g.
`S/(S+k)`) is *still* purely a function of `frequency` alone — the
normalization reshapes the curve, it does not introduce a second
independent variable. Concretely: for any two transitions `X`, `Y`,
`transition_rarity(X) < transition_rarity(Y)` if and only if
`surprisal(X) < surprisal(Y)`. They can never disagree on ranking. This
is proven directly by `TestMarkovSurprisalIsMonotonicReparameterizationOfRarity`
in `internal/anomaly/anomaly_test.go` (task 028's own "Critical
Duplication Test," mandated by the task brief).

**Conclusion: implementing raw or normalized `-log(P(B|A))` as a
second, independently-weighted anomaly signal would duplicate
`transition_rarity`'s evidence, not add to it.** Per the task's own
explicit instruction, this is documented here rather than silently
built around.

## What genuinely differs, and what this task actually builds

Even though `markov_surprisal` carries zero new *ranking* information
over `transition_rarity`, it is not valueless — it differs in exactly
one respect: **curve shape, not evidence.** `1 - frequency` compresses
severity sharply near `frequency -> 0`: a transition seen 1-in-100 and
one seen 1-in-100,000 both read as `~0.99`/`~0.99999` — visually almost
indistinguishable in a linear scale, and, more importantly, numerically
almost indistinguishable to `combine()`'s noisy-OR once multiplied by a
weight. `-log2(frequency)`, before normalization, keeps growing without
bound as `frequency` shrinks — it preserves resolution deep into the
rare tail that the linear mapping loses. After bounding it into
Trustvian's `[0,1)` signal range (see "Normalization" below), that
extra tail resolution is retained in relative terms, even though the
final numbers are still a monotonic function of the same `frequency`.

This is a legitimate reason to offer an **alternative severity curve**
for the identical evidence — useful to an operator who wants a
security scoring shape that differentiates "very rare" from
"extraordinarily rare" more sharply than a linear mapping does. It is
not a reason to score both curves independently. This task therefore
builds `markov_surprisal` as a **second, mutually exclusive scoring
curve over the same evidence transition_rarity already reads** — see
"Double-counting decision" below — not a new, additively-combined
signal.

## Mathematical definition

```
state:            (predecessor A, destination B) — identical to task 026, first-order only
numerator:        PredecessorCounts_B[A]                 (task 025 state, reused)
denominator:      OutgoingTransitionTotal_A               (task 026 state, reused)
P(B|A):           numerator / denominator                 (identical to transition_rarity's frequency)
raw surprisal:    S = -log2(P(B|A))                        (bits; math.Log2, stdlib only)
normalization:    normalized = S / (S + k)
k:                log2(max(Config.MinTransitionObservations, 2))
final range:       [0, 1)
```

**Why `k = log2(max(MinTransitionObservations, 2))`, not an invented
constant.** [ADR 0011](0011-transition-rarity-statistic-and-orientation.md)'s
own "Minimum support" section already established the natural
resolution floor this signal operates at: with `MinTransitionObservations`
samples, the smallest reliably-resolvable nonzero frequency is
`1/MinTransitionObservations`. Choosing `k` so that a transition
observed at exactly that floor frequency maps to `normalized = 0.5`
(the midpoint of the output range) ties the one free constant this
formula needs directly to a parameter this signal *already* depends on
for its cold-start gate, rather than inventing a second, independent
magic number — the "no arbitrary formulas" bar this codebase already
holds every other scoring constant to (see `internal/anomaly.combine`'s
own rationale comments). `max(..., 2)` guards `log2` against `0`/`1`
input (an operator setting `MinTransitionObservations` to `0` or `1`),
mirroring `transitionRaritySignal`'s own existing defensive floor
against a misconfigured threshold.

## Zero probability

Never computed. `markovSurprisalSignal` shares `transitionRaritySignal`'s
exact gate: `count == 0` is `transition_deviation`'s domain (task 025),
not this signal's — surprisal is only ever evaluated once `count > 0`,
which structurally guarantees `frequency > 0`, hence `S` is always
finite and `normalized` is always in `[0, 1)`. No epsilon floor, no
Laplace smoothing: both would only be needed to handle `frequency = 0`,
which this signal never receives in the first place, per §13/§15 of
the task's own brief ("if unseen transitions are already handled by
`transition_deviation`, smoothing may not be needed — prefer simple
semantics").

## Cold start

Reuses `Config.MinTransitionObservations` (task 026's own field) as
the minimum-support gate — the identical denominator, so no new
threshold field is introduced (avoiding the "configuration
proliferation" the task brief explicitly warns against). Below the
gate, no Markov signal at all — not maximum anomaly, not a weak
reading.

## Double-counting decision

**Chosen: Markov replaces rarity's *scoring contribution* when
enabled — explicitly, in code, not by documentation-only convention.**
When `Config.MarkovWeight > 0`, `anomaly.Score` computes both
`transition_rarity` and `markov_surprisal` (both remain visible in
`Anomaly.Contributors` — hiding either would cost explainability for
no benefit), but forces `transition_rarity`'s own contribution to
`combine()` to zero for that call, regardless of what
`Config.TransitionRarityWeight` is separately set to. This is
deliberately not left to operator discipline ("just don't set both
weights") — CLAUDE.md's "security decisions must be explainable,
deterministic" bar, and this codebase's own precedent of failing
closed on misconfiguration (`policy.Evaluate`), argue for a structural
guarantee over a documentation-only one: a caller cannot double-count
this evidence by any combination of the two weight fields, because the
code itself enforces mutual exclusivity of their *scoring* contribution
(not their *visibility*). See
`TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring` for
the proof.

The alternative of "Markov enriches metadata but never scores" (task
brief's option 2) was considered and rejected: it would make
`MarkovWeight` a config field with no scoring effect, which contradicts
this codebase's own "don't add a config knob without a current
caller/effect" convention (`.claude/rules/go.md`) — an operator who
sets a nonzero weight and sees no scoring change at all is a worse
surprise than one whose weight choice deterministically switches which
curve scores the same evidence.

## Relationship to transition rarity

**Shares evidence, offers an alternative curve, never independently
weighted alongside it.** Not "replaces" in the sense of removing
`transition_rarity` (both remain computed, reported, and independently
selectable via their own weight); not "enriches" in the sense of pure
metadata (its weight has a real, structural scoring effect once
enabled); not "genuinely separate evidence" (proven false by the
monotonic-reparameterization analysis above).

## Relationship to bounded n-grams (task 027)

Completely orthogonal, not interacting. `ngram_deviation`/`ngram_rarity`
answer a *different* question — "does the specific two-fingerprint
predecessor pair `(grandparent, predecessor)` lead here" — using
different state (`TrigramCounts`/`TrigramContinuationTotal`) entirely.
Task 028 does not touch that state, does not extend Markov scoring
over trigrams, and does not merge the two models — per the task
brief's own explicit instruction. Both can co-fire on the same event
(exactly like `transition_deviation`/`ngram_deviation` already can,
per ADR 0012), and `combine()`'s existing clamped noisy-OR keeps that
bounded, unchanged by this task.

## Why first-order only

Matches task 025/026's own scope boundary exactly: `state(t-1) ->
state(t)`, nothing more. No higher-order Markov, no hidden Markov
model, no stationary distribution, no Viterbi, no Monte Carlo, no
Bayesian inference — none of these are needed to answer the single
question this signal exists to answer (an alternative severity curve
over an already-computed first-order frequency), and building any of
them here would be exactly the speculative complexity CLAUDE.md and
this milestone's own non-goals warn against.

## Consequences

- **No new baseline state.** `markovSurprisalSignal` reads
  `PredecessorCounts`/`OutgoingTransitionTotal` — the identical task
  025/026 state `transitionRaritySignal` already reads. No new map, no
  new scalar, no new bound to reason about.
- `anomaly.Config` gains exactly one new field, `MarkovWeight float64`
  (defaults to `0`, the same "ships opt-in" precedent every prior
  signal weight in this package established). No new minimum-support
  field.
- `Score()` gains one line of new logic beyond calling the new signal
  function: forcing `transition_rarity`'s `Weight` to `0` in the
  `Signal` passed to `combine()` whenever `MarkovWeight > 0`.
- Existing `v0.5`/task-025/026/027 callers see byte-for-byte unchanged
  `Score` output: `MarkovWeight` defaults to `0`, at which point the
  forced-zero rule is a no-op (there is nothing to suppress) and
  `transition_rarity` behaves exactly as before this task.
