# 012 — Dedicated Security Test Suite

**Milestone:** v0.1 · **Depends on:** none · **Blocks:**
[013](013-oss-v01.md)

## Objective

Organize security-relevant testing into a suite explicitly structured
around threats, and close the concrete gaps
[SECURITY.md](../SECURITY.md) doesn't yet have direct tests for:
malformed/extreme input handling and resource exhaustion. Most named
threats already have tests — this task is about making that fact
verifiable and complete, not starting from zero.

## Why

[SECURITY.md](../SECURITY.md) (written during the prior
architecture-hardening pass) already classifies every threat the
roadmap brief names — telemetry spoofing, identity confusion, replay,
baseline poisoning, malicious agents/privilege escalation, policy
bypass, future multi-tenant isolation — as implemented, partially
addressed, or explicitly deferred, each pointing at the specific test
proving it. Two things that document names
(malformed events / extreme input values, and — implicitly, via
"concurrency issues" — resource exhaustion) are **not** in that
threat list at all today, and the existing tests for the other threats
are scattered across each package's own `_test.go` files rather than
organized as a recognizable, auditable "security suite."

## Scope

1. **Add the two missing threats to [SECURITY.md](../SECURITY.md)**:
   malformed/extreme input values, and resource exhaustion — with the
   same implemented/deferred rigor every other entry already has.
2. **Malformed/extreme input tests**: `event.Event.Validate()` and
   `Engine.Analyze` under adversarial input — `IdentityConfidence` of
   `NaN`/`+Inf`/`-Inf`, empty/extremely long strings in `Actor.ID`/
   `Operation.Name`/`Target.Name`, deeply nested or very large
   `Attributes` maps, negative `duration_ms`. Every case must either
   be correctly rejected by `Validate()` or correctly, safely handled
   (no panic, no NaN propagating silently into `Trust.Score`) —
   decide per-case which is correct behavior and assert it explicitly,
   not just "doesn't crash."
3. **Resource exhaustion smoke tests**: an `Attributes` map with a
   very large number of keys doesn't cause unbounded allocation
   *beyond* what's proportional to the input (i.e., `Engine.Analyze`'s
   cost scales with input size, it doesn't amplify it); an actor
   producing an unbounded number of distinct fingerprints doesn't
   panic (ties into [011](011-performance.md)'s memory-growth
   characterization, but the *safety* property — no panic, no
   deadlock — is this task's concern, the *growth curve* is 011's).
4. **Explicit cross-actor isolation test**: two actors with otherwise
   identical stable-feature shapes (same operation, same target) but
   different `ActorID`/`Environment` never share `Baseline` state —
   this is true by construction (`baseline.Key`'s composite shape) but
   not directly, explicitly tested end-to-end through `Engine` today;
   this task adds that direct proof.
5. **Consolidate**, don't duplicate: where a threat already has a
   clear test (baseline poisoning →
   `TestObserveLearnsOnlyFromEligibleDecisions`/
   `TestAnalyzeSensitiveTargetFloorEndToEnd` in `engine_test.go`;
   policy bypass → the three fail-closed tests in
   `internal/policy/policy_test.go`), this task's job is to *reference*
   them clearly (a short index, e.g. a table at the top of a new
   `SECURITY_TESTS.md` or within `SECURITY.md` itself) rather than
   rewrite or relocate working tests.

## Non-Goals

- No fuzzing framework (`go-fuzz`/native Go fuzzing corpus) — the
  malformed-input cases above are enumerable, table-driven test cases,
  not an open-ended fuzz target; if real fuzzing is justified later,
  that's a separately-scoped task.
- No penetration-testing or external security audit — this is
  self-testing within the existing `go test` suite.
- No new protections invented purely to have something to test —
  every new test in this task validates *existing* behavior (input
  validation, key scoping) or documents a *deliberately deferred* gap;
  it does not add new runtime logic beyond what's needed for a test to
  pass (e.g., if `Validate()` doesn't already reject `NaN`
  `IdentityConfidence`, adding that rejection is in scope precisely
  because it's a validation gap, not a new feature).

## Technical Requirements

- Extreme-input tests must assert specific expected behavior (rejected
  with a specific error, or accepted and produces a specific, bounded
  score) — "doesn't crash" alone is an insufficient assertion for a
  security test.
- Tests should be runnable as part of the normal `go test ./...`
  loop — no separate opt-in security-test flag, keeping them exercised
  by default rather than easy to forget.

## Tests

This task *is* the tests — see Scope items 2–4 above for the concrete
list. Locations: `event/event_test.go` (input validation),
`engine_test.go` (cross-actor isolation, end-to-end resource-exhaustion
smoke tests).

## Benchmarks

- If the resource-exhaustion smoke tests reveal a cost worth tracking
  ongoing (e.g. large-`Attributes`-map `Analyze` cost), add it as a
  benchmark rather than leaving it as a pass/fail test alone — decide
  during implementation based on what's found.

## Documentation

- [SECURITY.md](../SECURITY.md): add the two new threat entries;
  update every existing entry's test reference if this task
  consolidates/renames anything (it shouldn't need to, per Non-Goals).

## Acceptance Criteria

- [SECURITY.md](../SECURITY.md) covers all threats named in the
  roadmap brief (telemetry spoofing, replay, baseline poisoning,
  identity confusion, policy bypass, malformed events, extreme input
  values, concurrency issues, resource exhaustion), each with a
  test reference or an explicit "future mitigation" label — no
  threat left undocumented.
- `go test ./... -race` green including all new tests.
- The cross-actor isolation property is proven by an explicit,
  readable test, not just implied by `baseline.Key`'s shape.
