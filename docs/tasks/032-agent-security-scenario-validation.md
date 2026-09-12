# 032 — Agent Security Scenario Validation

**Milestone:** v0.7 · **Depends on:** [014](014-ai-agent.md),
[030](030-approval-aware-policy-semantics.md),
[031](031-delegation-behavioral-semantics.md) (this task validates
their composition, adds nothing new to any of them) · **Blocks:**
[033](033-v07-stabilization-release-gate.md) — the stabilization gate
audits this task's findings, including the public-API gap below (and
closes it — see [033](033-v07-stabilization-release-gate.md) and [ADR
0017](../adr/0017-public-anomaly-configuration-boundary.md)), before
`v0.7.0` tags.

## Objective

Answer this task's own question with tests and a runnable example, not
a design doc: *can Trustvian meaningfully detect and govern realistic
AI-agent security behavior by composing capabilities it already has?*
This is a scenario-validation and integration-hardening task, not a
feature task — its deliverable is proof of composition, not a new
detector.

## Why

Tasks 014, 030, and 031 each proved their own mechanism works in
isolation (agent context foundation, approval policy, delegation
evidence). None of them proved the three work *together*, under
realistic attack shapes, without colliding, double-counting, or
requiring a bespoke scenario-specific engine. That composition is the
actual product claim — "Trustvian evaluates runtime behavior instead
of relying only on identity," applied to AI agents specifically — and
it had not yet been exercised end-to-end in one flow before this task.

## Scenario Matrix

| Scenario | Mechanism Reused | Expected Evidence | Decision Mechanism |
|---|---|---|---|
| 1. Unexpected privileged tool | `categorical_novelty`/`transition_deviation` (v0.1/task 025) | Contributors present, no new detector | `Policy`/`Trust`, unchanged |
| 2. Sensitive read → external post sequence | `ngram_deviation` (task 027) | Fires despite both pairwise hops being familiar | `Policy`/`Trust`, unchanged |
| 3. Approval violation | `Policy` approval condition (task 030) | `Anomaly`/`Trust` unaffected; `Decision` differs | `Policy.Evaluate`'s `Unless` rule |
| 4. Unexpected delegator | `delegation_deviation` (task 031) | Fires; does **not** force `BLOCK` on its own | `Policy`'s own choice (proven both ways) |
| 5. External destination drift | `categorical_novelty` via `Target.Category` | Fires via normalized category, no raw-URL fingerprinting | `Policy`/`Trust`, unchanged |
| Combined: delegation + sequence + approval | all of the above, composed | delegation + n-gram evidence present, orthogonal | `Policy`'s approval rule (deterministic) |

## Scope

```text
Existing Engine.Analyze/Observe (unchanged)
      ↓
Scenario 1-5 + combined case, expressed as integration tests
composing existing signals/Policy/Alert — no new production code
      ↓
One new, fully-public example (examples/ai-agent-security/)
demonstrating the one scenario expressible through public API alone
      ↓
Documentation: scenario matrix, findings, gap report
```

- `scenario_test.go` (new, repository root, `package trustvian_test`):
  eleven tests covering the five scenarios, the combined case, a
  combined poisoning regression, a session-cardinality re-run, a
  non-agent regression, an identity-confidence-independence check, and
  a no-correlated-evidence-explosion audit under every signal weight
  at once.
- `examples/ai-agent-security/` (new): the one scenario fully
  expressible through public API (`event`, `config`, root `trustvian`,
  `alert` — no `internal/*` import) — task 030's approval mechanism,
  end-to-end, via `config.PolicyConfig` → `config.CompilePolicy`.
- Zero changes to `internal/anomaly`, `internal/baseline`,
  `internal/policy`, `internal/trust`, `alert`, `config`, `event`.

## Non-Goals

- **No new detector, anomaly signal, or scoring formula.** Every
  scenario is expressed with signals tasks 014/025/027/030/031 already
  built.
- **No second policy engine, no `AgentPolicyEngine`.** `Policy`
  composition uses the existing `Rule`/`Condition`/`Unless` mechanism
  throughout.
- **No delegation authentication, authorization, or graph/multi-hop
  modeling.** Scenario 4 explicitly re-proves task 031's own
  limitation: `delegation_deviation` is evidence, never authorization.
- **No approval workflow, persistence, or human-notification
  mechanism.** Scenario 3 explicitly re-proves task 030's own
  limitation.
- **No session risk engine.** `SessionID` stays context-only; the
  session-cardinality regression re-confirms this, it does not extend
  it.
- **No prompt-injection detector, no prompt analysis, no LLM
  inference, no embeddings/vector DB, no MCP, no Control.**
- **No new public API**, with one documented exception this task
  explicitly declined to build (see Findings below) rather than adding
  speculatively.
- **Task 033 (`v0.7` Stabilization & Release Gate) is not started.**

## Findings — a genuine public-API gap, reported rather than patched

While designing the fully-public example (§18/§19/§34 of this task's
own brief demanded at least one scenario work through public API
alone), this task found that `anomaly.Config` — unlike `policy.Policy`,
which got a public `config.CompilePolicy` path in `v0.5` (ADR 0008) —
has **no** public-API equivalent. `trustvian.WithAnomalyConfig` takes
an `internal/anomaly.Config` value directly; there is no
`config.AnomalyConfig`/`CompileAnomalyConfig`. Concretely, an OSS
consumer outside this module has no way to raise `DelegationWeight`,
`NGramWeight`, `TransitionWeight`, or any other `v0.6`/`v0.7` signal
weight above its default-`0`, opt-in value — those signals are fully
implemented and fully tested (inside this module), but structurally
invisible to a pure-public-API caller today.

This is a real usability gap, not a correctness defect: every signal
computes correctly and every test passes. Per this task's own explicit
instruction ("if a public API addition is required to express a
legitimate scenario: STOP and explain why existing API is insufficient
before adding it"), this task does **not** add a `config.AnomalyConfig`
speculatively — doing so is a real design decision (schema shape,
validation rules, versioning) that deserves its own scoped task with
its own review, not a byproduct of a scenario-validation pass. The
fully-public example
([`examples/ai-agent-security`](../../examples/ai-agent-security/))
is scoped to what's genuinely public today (task 030's approval
mechanism) and says so explicitly in its own README, rather than
working around the gap with an internal import or overstating what an
OSS user can actually configure. Task 033 (or a later, dedicated task)
should decide whether this gap blocks `v0.7.0` or is explicitly
deferred — this task surfaces the finding, it does not adjudicate it.

## Security Assertions

Each proven by a specific test, not merely documented:

- **Familiar != authorized.** `TestScenarioApprovalViolation`: a fully
  mature, familiar `shell.execute` is still `BLOCK`ed without approval.
- **Novel != malicious.** `TestScenarioUnexpectedDelegator`: the
  identical novel-delegator evidence appears under both a risk-gated
  policy (which blocks) and a permissive one (which doesn't) — the
  evidence itself never forces enforcement.
- **`ApprovalStatus: approved` != authenticated provenance.**
  Unchanged from task 030; no scenario in this task treats it as
  verified. `TestScenarioApprovalViolation`'s alert check operates on
  the resulting `Decision`, never on a claim that approval was
  cryptographically verified.
- **`DelegatedFrom` != authenticated provenance.** Unchanged from task
  031; `TestScenarioUnexpectedDelegator`/`TestScenarioCombinedDelegationSequenceApproval`
  treat it as behavioral evidence only throughout.
- **`IdentityConfidence` stays independent.**
  `TestScenarioIdentityConfidenceStaysIndependent` confirms it
  influences only `Trust.Score`'s own multiplicative term, never
  `delegation_deviation`'s value or the approval rule's evaluation.

## Test Plan

`scenario_test.go` (new, +11):
`TestScenarioUnexpectedPrivilegedTool`,
`TestScenarioSensitiveExfiltrationSequence`,
`TestScenarioApprovalViolation`, `TestScenarioUnexpectedDelegator`,
`TestScenarioExternalDestinationDrift`,
**`TestScenarioCombinedDelegationSequenceApproval`** (the mandatory
combined scenario), `TestScenarioCombinedPoisoningRegression`,
`TestScenarioIdentityConfidenceStaysIndependent`,
`TestScenarioNoCorrelatedEvidenceExplosionUnderEveryWeight`,
`TestScenarioSessionCardinalityRegression`,
`TestScenarioNonAgentRegressionUnaffected`.

All run under `go test ./... -race -count=1`.

## Example Plan

`examples/ai-agent-security/` (new): trains `shell.execute` to full
familiarity (25 `Analyze`+`Observe` calls, all `Approved`), then
evaluates the identical, now-familiar call twice at the same
steady-cadence timestamp — once `Approved` (allowed), once `Denied`
(blocked) — via a `config.PolicyConfig` compiled with
`config.CompilePolicy`, plus an `alert.Rule` matching the resulting
`Decision`. Verified with real, captured `go run .` output (not
hand-written); added to `examples/README.md`'s index and its own "A
note on `Decision`" section, corrected to describe the now-real
`config.CompilePolicy` path.

## Performance

No production code changed — this task adds only tests, one example,
and documentation. `go test -bench=. -benchmem ./...` re-run to
confirm zero regression; no new benchmark was needed since no new
production hot path exists. Expected overhead: **none**.

## Documentation

- [docs/ROADMAP.md](../ROADMAP.md) § v0.7: task 032 marked done,
  including the public-API gap finding; 033 remains scoped, not
  implemented.
- [docs/SECURITY.md](../SECURITY.md): scenario matrix added,
  cross-referencing the same threats/mitigations tasks 014/030/031
  already documented — no new claims.
- [README.md](../../README.md) / [CHANGELOG.md](../../CHANGELOG.md):
  this task's actual deliverable (validation, not a new detector)
  accurately described.
- [examples/README.md](../../examples/README.md): new example added
  to the index; "A note on `Decision`" corrected.
- [trustvian-project-spec.md](../../trustvian-project-spec.md) § 16:
  a short note that `v0.7`'s capabilities are now validated in
  combination, plus the public-API gap.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new test above.
- `TestScenarioCombinedDelegationSequenceApproval` passes — the
  mandatory combined-scenario proof.
- `TestScenarioUnexpectedDelegator` passes both its risk-gated and
  permissive-policy sub-cases — the mandatory "novel != automatic
  BLOCK" proof.
- `TestScenarioNoCorrelatedEvidenceExplosionUnderEveryWeight` passes —
  ADR 0013's mutual-exclusion guarantee holds with every `v0.6`/`v0.7`
  signal weight enabled at once.
- `examples/ai-agent-security` builds and runs (`go run .`), verified
  via `make examples`, with real captured output in its README.
- No new package, no new pipeline stage, no new detector, no new
  dependency — verified by reviewing the diff against this constraint
  (`git diff -- go.mod go.sum processor/go.mod processor/go.sum`
  empty).
- The `anomaly.Config` public-API gap is documented, not silently
  patched with a speculative new public type.
