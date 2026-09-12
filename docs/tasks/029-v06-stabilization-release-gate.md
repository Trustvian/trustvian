# 029 — v0.6 Stabilization & Release Gate

**Milestone:** v0.6 · **Depends on:**
[025](025-sequence-analysis-foundation.md)–[028](028-markov-transition-scoring.md)
· **Blocks:** `v0.6.0` (tag it, or start `v0.7`, only once this gate
passes) · **Mirrors:** [024](024-v05-release-gate.md), the `v0.5`
release gate — same shape, applied to `v0.6`.

## Objective

Not new functionality — a verification and documentation-synchronization
pass tying tasks 025–028 together into a release-ready `v0.6.0`
candidate. This task's deliverable is an audit, a small number of
justified fixes, and synchronized documentation — not new detection
capability. Feature freeze was in effect for the entire pass: no
4-gram, no higher-order Markov, no ML, no new configuration surface.

## Why

`v0.1`'s ([013](013-oss-v01.md)) and `v0.5`'s ([024](024-v05-release-gate.md))
own release gates established the pattern: verify the whole milestone
together, not just each task in isolation, because cross-task
interactions (double-counted evidence, documentation cross-references,
a shared-gate benchmark side effect) are exactly the class of defect no
single task's own review can catch. `v0.6` had a specific, named risk
this gate existed to close before release: task 028's own duplication
analysis showed `markov_surprisal` and `transition_rarity` share
identical evidence — verifying that the mutual-exclusion mechanism
actually holds under every realistic combination, not just the
scenarios task 028's own tests constructed, was this gate's single
highest-priority item.

## Scope

- Verify every task 025–028 individually still meets its own
  acceptance criteria, on the current tree, not from memory.
- Audit the `markov_surprisal`/`transition_rarity` double-counting
  protection specifically, across the full behavioral-signal
  combination space, not just the pairwise case task 028's own tests
  constructed.
- Audit state bounds, counter safety, concurrency, ordering, actor
  isolation, cold start, and baseline-poisoning resistance across all
  four tasks together — confirming no combination of them introduces a
  gap no single task's own isolated review would have found.
- Run a fresh, full-repository benchmark matrix across every
  feature-enablement combination (default, each signal individually,
  full behavioral) — not just each task's own before/after delta —
  specifically to answer whether a disabled (`Weight == 0`) v0.6 signal
  still costs its computation on the hot path, and whether that is the
  correct, consistent architectural choice.
- Audit the actual, current OSS configuration surface for every v0.6
  capability (Go API, declarative YAML, CLI, OTel Collector processor)
  against this repository's own established `v0.5`
  Policy/Alert-configuration precedent, and decide explicitly whether
  the absence of declarative config for v0.6 signals is a release
  blocker.
- Audit the public API surface changed since `v0.5.0`
  (`git diff v0.5.0..HEAD -- '*.go'`) for accidental exposure or
  compatibility breakage.
- Audit dependencies changed since `v0.5.0`.
- Re-run a full documentation consistency check across every `.md`
  file this milestone touched or references (stale "first task
  done"-style phrasing, dangling ADR cross-references, task-file
  claims that don't match implementation).

## Findings and Resolutions

- **Stale roadmap wording (HIGH, fixed).** `docs/ROADMAP.md`'s own
  top-of-file summary paragraph still read "`v0.6` has its first task
  done — [025]" after tasks 026–028 had already shipped. Corrected to
  reflect all four feature slices being done, with stabilization named
  explicitly.
- **Markov/rarity double counting (audited, no defect found — the
  gate's own primary concern).** Re-verified from source (not from
  memory) that `anomaly.Score` forces `transition_rarity`'s
  `Signal.Weight` to `0` whenever `Config.MarkovWeight > 0`,
  unconditionally, regardless of `Config.TransitionRarityWeight`'s own
  value. Confirmed by re-running
  `TestScoreMarkovAndTransitionRarityAreMutuallyExclusiveInScoring` and
  `TestMarkovSurprisalIsMonotonicReparameterizationOfRarity` and by a
  fresh full-combination benchmark
  (`BenchmarkEngineAnalyzeFullBehavioral`, new this pass) exercising
  every weight nonzero simultaneously — no amplification beyond what
  each independent signal already contributes was found.
- **Programmatic-only v0.6 configuration (audited, confirmed
  intentional, not a blocker).** Every v0.6 signal weight is reachable
  only via `trustvian.WithAnomalyConfig` from code living inside this
  module (`anomaly.Config` is `internal/anomaly.Config`) — verified
  this is not new to `v0.6`: `FrequencyWeight` (`v0.1`) and
  `TimePatternWeight` (`v0.3`) already carry the identical limitation,
  already documented in `engine.go`'s own doc comment, `README.md`'s
  own "Limitations" section, and `examples/frequency-abuse`'s own
  comment. Declarative (YAML/CLI/Collector) integration remains
  deliberately deferred, per every task 025–028's own Non-Goals — this
  gate reaffirms that decision rather than silently changing it (see
  "Config Completeness" below).
- **Disabled-signal computation cost (audited, confirmed intentional,
  documented, not changed).** Measured directly: a user who never
  raises `MarkovWeight` (or any other v0.6 weight) above its `0`
  default still pays that signal's full lookup/arithmetic cost on
  every `Analyze` call with sufficient history — confirmed by a fresh
  benchmark matrix (`docs/PERFORMANCE.md § v0.6 stabilization pass`).
  This is the same "compute for explainability regardless of weight"
  convention `FrequencyWeight`/`TimePatternWeight` already established
  before any v0.6 signal existed. Changing it for Markov alone would
  be the exact signal-specific inconsistency this gate exists to catch,
  not introduce; changing it for all eleven signals would be a
  whole-package behavior change out of scope for a stabilization pass.
  Left unchanged, documented in full.
- **No unbounded v0.6 state found.** `PredecessorCounts`,
  `TrigramCounts`, and `TrigramContinuationTotal` are each
  independently bounded at 64 entries (`maxPredecessors`/
  `maxTrigramPredecessors`). `Baseline.Fingerprints` and
  `store.InMemory`'s shard map remain unbounded by design — a
  pre-existing, already-documented `v0.1` characteristic
  (`docs/SECURITY.md § Resource exhaustion`), not introduced or
  worsened by `v0.6`.
- **No new dependency, no public API change since `v0.5.0`.** Verified
  by `git diff v0.5.0..HEAD -- '*.go'` (only `internal/anomaly`,
  `internal/baseline`, and root-package `_test.go` files changed — zero
  exported root-package/`event`/`config`/`alert` symbols touched) and
  `git diff v0.5.0..HEAD -- go.mod go.sum processor/go.mod
  processor/go.sum` (the only change is `processor/go.mod`'s own
  already-documented `v0.3.0 -> v0.5.0` bump, a `v0.5`-era follow-up
  that chronologically lands after the `v0.5.0` tag, not `v0.6` work).

## Non-Goals

- No feature work of any kind — no 4-gram, no higher-order Markov, no
  HMM, no ML, no graph analysis, no declarative config integration (see
  the finding above for why this was evaluated and deliberately not
  built here).
- No `v0.7` work of any kind.
- No dashboard, RBAC, multi-tenant management UI, or any other
  Enterprise-scope capability.
- No refactor of stable code merely because an alternative design is
  possible — every fix in this gate addresses a demonstrated
  documentation-drift or verification gap, not a stylistic preference.

## Tests

- The full existing root test suite, run fresh: `go test ./...
  -race -count=1`.
- `processor/`'s test suite, run independently with `GOWORK=off`
  against the real, published `v0.5.0` dependency (no local workspace).
- No new regression tests were required: the audit found no
  undemonstrated defect. Two new benchmarks
  (`BenchmarkEngineAnalyzeTransitionDeviation`,
  `BenchmarkEngineAnalyzeFullBehavioral`) were added to complete the
  performance matrix this gate's own audit needed — not correctness
  tests, and not new production code.

## Benchmarks

Full feature-combination matrix (default, each signal individually,
full behavioral) — see
[docs/PERFORMANCE.md § v0.6 stabilization pass](../PERFORMANCE.md#v06-stabilization-pass-task-029-full-feature-combination-matrix)
for the complete table and the disabled-signal-computation decision
this gate's own audit reached.

## Documentation

- [docs/ROADMAP.md](../ROADMAP.md): stale top-of-file "first task
  done" wording corrected; `v0.6` milestone status finalized.
- [docs/PERFORMANCE.md](../PERFORMANCE.md): new full-combination
  benchmark matrix and the disabled-signal-computation architectural
  decision, documented explicitly.
- This task file (new).

## Release Checklist

```text
v0.6.0 release gate

[x] scope complete (025, 026, 027, 028)
[x] root tests                          (go test ./...)
[x] root race                           (go test -race ./...)
[x] root vet                            (go vet ./...)
[x] root formatting                     (gofmt -l .)
[x] benchmarks reviewed                 (full feature-combination matrix; no unexplained regression)
[x] processor build (GOWORK=off)        (against the real, published v0.5.0)
[x] processor tests (GOWORK=off)
[x] processor race (GOWORK=off)
[x] processor vet (GOWORK=off)
[x] docs consistent                     (stale roadmap wording found and fixed; see Findings above)
[x] CHANGELOG accurate                  (Unreleased section reviewed, already accurate — no change needed)
[x] public API review                   (zero exported symbols changed since v0.5.0 — verified via git diff)
[x] dependency review                   (zero new dependencies since v0.5.0 — verified via git diff)
[x] double-counting audit               (Markov/rarity mutual exclusion re-verified from source and by fresh benchmark; no defect found)
[x] state-bounds audit                  (all new v0.6 maps independently bounded at 64 entries; no unbounded sequence state found)
[x] config-completeness decision        (programmatic-only confirmed intentional, consistent with FrequencyWeight/TimePatternWeight precedent; not a blocker)
[x] security review                     (docs/SECURITY.md already covers cold start, poisoning, isolation, ordering, numeric safety, double counting for all four tasks)
[ ] human creates/pushes v0.6.0 tag         — not performed; explicitly out of scope for this task
[ ] GitHub release                          — not performed; explicitly out of scope for this task
```

## Acceptance Criteria

- Every dependency task (025–028) independently verified complete
  against its own acceptance criteria, on the current tree.
- `go build`/`go vet`/`go test -race`/`gofmt -l` all clean at the
  repository root.
- `processor/` builds and tests clean with `GOWORK=off`, no workspace,
  against the real, published `v0.5.0` dependency.
- The double-counting protection between `transition_rarity` and
  `markov_surprisal` is verified from source and by a fresh
  full-combination benchmark, not merely re-asserted from task 028's
  own report.
- No unbounded `v0.6`-introduced behavioral state exists.
- No public API or dependency change since `v0.5.0` beyond the
  already-documented `processor/go.mod` follow-up.
- The full-repository documentation consistency check finds and fixes
  genuine drift (the stale "first task done" wording) without
  rewriting historical task records.
- A release decision (`READY`/`NOT READY`) is reached and reported,
  with no ambiguous language.
