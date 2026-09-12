# 030 — Approval-Aware Policy Semantics

**Milestone:** v0.7 · **Depends on:** [014](014-ai-agent.md) (adds
`event.ApprovalStatus`, this task's first real consumer) ·
**Blocks:** [031](#roadmap-successors-illustrative)–onward (delegation
behavioral semantics and agent security scenario validation both
assume approval enforcement already works) · **Mirrors:** the
`Unless`-based exception pattern `internal/policy` already
established in [task 006](006-policy.md).

## Objective

Give `event.ApprovalStatus` its first real consumer — entirely inside
`internal/policy`, with zero new anomaly signal, zero new pipeline
stage, and zero new persistence — answering the design question this
task's own brief posed before any code was written: *how can
Trustvian deterministically enforce approval requirements using
trustworthy event context without becoming an approval workflow
engine?* See [ADR
0015](../adr/0015-approval-as-policy-evidence-not-behavioral-anomaly.md)
for the full design.

## Why

Task 014 added `ApprovalStatus` as a typed, optional field, explicitly
reserved for "a later signal to consume once one has a concrete
design" — this is that design. The task's own brief drew a sharp line
before any implementation: "is this behavior unusual?" (`anomaly`) and
"is this behavior allowed?" (`policy`) are different questions, and
`shell.execute` can be completely familiar to a `Baseline` while still
lacking a required approval. Blurring the two — inflating an anomaly
score to represent a deterministic authorization gap — would be a
correctness defect, not a stylistic one. Equally important: prior to
this task, the only way to express an approval-like condition in a
`Policy` was the ad hoc `Attributes: {"approval": "human"}` convention
`config/validate_test.go`'s own `fullValidConfig()` fixture already
used — a string key with no meaning Trustvian itself understands.
This task gives that intent a real, validated, typed home.

## Scope

```text
Event (unchanged shape — Context.ApprovalStatus already exists, task 014)
      ↓
Engine.Analyze threads ev.Context.ApprovalStatus into policy.Input
      ↓
policy.Condition gains ApprovalStatus — one more equality-match field,
identical in kind to ActorType/TargetName/MinRiskLevel/Attributes
      ↓
Existing Rule.Unless mechanism expresses "operation X requires
approval": When matches the operation, Unless matches
ApprovalStatus: Approved
      ↓
Trust / Anomaly / Baseline — all unchanged, proven independent by test
      ↓
Decision → Alert  (unchanged — an approval BLOCK is a BLOCK like any other)
```

- `internal/policy`: `Input` gains `ApprovalStatus event.ApprovalStatus`;
  `Condition` gains `ApprovalStatus event.ApprovalStatus`, checked in
  `Matches` the same way every other field already is.
- `engine.go`: `Analyze` populates the new `Input.ApprovalStatus` from
  `ev.Context.ApprovalStatus` — the same place `Attributes` was
  already threaded through.
- `config`: `PolicyCondition` gains `ApprovalStatus string`
  (`yaml:"approval_status"`); `CompilePolicy`'s `compileCondition`
  translates it; `Validate` rejects any value outside the four
  non-empty `event.ApprovalStatus` constants (`not_required`,
  `required`, `approved`, `denied`) with a new sentinel,
  `ErrInvalidApprovalStatus`.
- **No new Rule-level primitive.** "This operation requires approval"
  is expressed entirely with the existing `When`/`Unless` mechanism —
  see ADR 0015's worked example. Policy, never the event, is
  authoritative over whether an operation requires approval: an event
  self-declaring `ApprovalNotRequired` has no power to exempt itself
  from a rule that requires `Approved`.

## Non-Goals

- **No approval workflow, UI, request flow, persistence, or
  human-notification mechanism.** Trustvian evaluates approval
  evidence; it does not grant approval or manage an approval process.
- **No `ApprovalStore`/`ApprovalRepository`/`PendingApproval`/
  `ApprovalHistory`.** Approval evidence is read once per `Analyze`
  call and never persisted across events.
- **No session-scoped approval cache.** `Context.SessionID` (task
  014) stays context-only; approval is re-evaluated per event.
- **No new anomaly signal** (`approval_deviation`/`approval_anomaly`/
  `approval_rarity`/`approval_baseline`/`approval_transition`) —
  approval enforcement is deterministic policy, not learned behavior.
- **No cryptographic verification of approval provenance, no OAuth/IAM
  integration, no signature checking.** `ApprovalStatus` remains
  untrusted, self-reported evidence — see ADR 0015's trust-boundary
  section and `docs/SECURITY.md`'s updated "Approval self-assertion"
  entry.
- **No coupling to `Actor.IdentityConfidence`.** The two represent
  independent evidence (who vs. whether this operation was
  authorized) and stay that way absent a concrete requirement.
- **No `AgentPolicyEngine`/`ApprovalPolicyEngine`.** Approval is one
  more `Condition` field inside the existing `internal/policy`
  evaluator.
- **No delegation semantics.** `DelegatedFrom` (task 014) is
  unaffected by this task; delegation-aware detection is
  [031](#roadmap-successors-illustrative)'s job, not this one's. This
  task's approval condition must not (and does not) treat
  `DelegatedFrom` as authenticated provenance.
- **No new alert mechanism.** An approval-triggered `BLOCK` flows
  through the existing `alert.Evaluate`/`alert.Condition{Decision:
  policy.DecisionBlock}` path unchanged.

## Technical Requirements

- `policy.Condition.ApprovalStatus`'s zero value (`""`) means "don't
  care", identical to every other `Condition` field's convention —
  it cannot itself distinguish "any approval state" from
  "specifically `ApprovalUnspecified`" (the same limitation
  `TargetName` already documents for "any target" vs. "specifically
  empty").
- The Approval Matrix is fixed and tested, not left to interpretation:

  | Event `ApprovalStatus` | Policy requires approval | Requirement satisfied |
  |---|---|---|
  | `Approved` | Yes | Yes |
  | `Denied` | Yes | No — fails closed |
  | `Required` | Yes | No — fails closed (functionally pending) |
  | `Unspecified` | Yes | No — fails closed (missing evidence) |
  | `NotRequired` | Yes | No — fails closed (event cannot override Policy) |

- A `Policy` with no approval-aware rule at all must be byte-for-byte
  unaffected by any `ApprovalStatus` value — approval context alone
  must never become an implicit, global policy.
- `Anomaly.Score`, `Trust.Score`, and `Fingerprint.ID` must be
  identical for two otherwise-identical events differing only in
  `ApprovalStatus` — only `Decision` may differ.
- An unrecognized `approval_status` string in configuration must be
  rejected by `Validate`/`CompilePolicy`, never silently accepted or
  silently mapped to any particular value (least of all `approved`).
- Zero new dependencies; zero changes to `internal/anomaly`,
  `internal/baseline`, `internal/trust`.

## Tests

`internal/policy/policy_test.go` (+7):
`TestConditionMatchesApprovalStatus`,
**`TestEvaluateApprovalRequiredExampleMatrix`** (the mandatory Approval
Matrix proof — all five `ApprovalStatus` values against the canonical
`Unless`-based rule),
**`TestEvaluateApprovalPolicyAuthorityEventCannotOverridePolicy`** (the
mandatory policy-authority regression — `ApprovalNotRequired` cannot
override a Policy that requires approval),
**`TestEvaluateApprovalFailSafeOnMissingEvidence`** (missing evidence
fails closed),
`TestEvaluateNoApprovalRuleConfiguredIsUnaffectedByApprovalStatus` (the
mandatory backward-compatibility regression).

`config/validate_test.go` (+4): `TestValidateAcceptsEveryApprovalStatus`,
`TestValidateRejectsInvalidApprovalStatus`,
`TestValidateRejectsUnknownApprovalStatusDoesNotSilentlyMapToApproved`
(the mandatory security regression — an unrecognized value is rejected,
never silently accepted), `TestValidateAcceptsUnsetApprovalStatusAsDontCare`.

`config/compile_test.go` (+1, and one existing test extended):
`TestCompileApprovalConditionViaUnless`;
`TestCompileConditionTranslatesEveryField` extended to cover
`ApprovalStatus`.

`engine_test.go` (+3):
**`TestAnalyzeAgentApprovalPolicyAllowsApprovedDeniesUnapproved`** (the
task's own mandatory AI-agent integration test — shell.execute trained
familiar, yet still gated by approval: Approved allows, Denied
blocks, proving behaviorally normal does not imply
policy-authorized),
**`TestAnalyzeApprovalPolicyBehavioralScoreIndependence`** (the
mandatory architectural regression — Anomaly/Trust/Fingerprint
identical across Approved/Denied, only Decision differs),
**`TestAnalyzeApprovalPolicyGenericNotHardCodedToAIAgent`** (the
mandatory non-agent-genericity test — a `service` actor is gated
identically to an AI agent).

All of the above run under `go test ./... -race -count=1`.

## Benchmarks

`internal/policy/policy_bench_test.go` (+1):
`BenchmarkEvaluateApprovalCondition` — confirmed `0 B/op, 0 allocs/op`,
identical to `BenchmarkEvaluateMatch`, proving approval evaluation adds
no allocation of its own (one more `string` equality comparison in
`Condition.Matches`, nothing more). `BenchmarkEngineAnalyze` re-run and
confirmed unchanged (`456 B/op, 17 allocs/op`) — this task adds a field
assignment on an existing `struct` literal, not a new allocation path.
See [docs/PERFORMANCE.md § v0.7 task
030](../PERFORMANCE.md#v07-task-030-approval-aware-policy-semantics).

## Documentation

- [docs/adr/0015-approval-as-policy-evidence-not-behavioral-anomaly.md](../adr/0015-approval-as-policy-evidence-not-behavioral-anomaly.md)
  (new): the full design, the Approval Matrix, the trust-boundary
  reasoning.
- [docs/policy-guide.md](../policy-guide.md): new "Example: an
  operation that requires approval" section; the YAML loader example's
  old ad hoc `attributes: {approval: human}` convention replaced with
  the real `approval_status` field.
- [docs/DOMAIN.md](../DOMAIN.md): `ApprovalStatus`'s entry updated to
  distinguish approval *state* from approval *evidence trust*, and to
  note its new (and only) consumer.
- [docs/SECURITY.md](../SECURITY.md): "Approval self-assertion" entry
  updated to reference this task's actual mechanism and its fail-closed
  guarantee, now that a real consumer exists.
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md): pipeline note on Policy
  gaining an approval-aware condition, not a new stage.
- [trustvian-project-spec.md](../../trustvian-project-spec.md) § 16:
  approval-aware deterministic policy marked implemented; approval
  workflow/granting/UI/persistence marked explicitly not implemented.
- [docs/ROADMAP.md](../ROADMAP.md) § v0.7: task 030 marked done; 031–033
  remain scoped, not implemented.
- [README.md](../../README.md) / [CHANGELOG.md](../../CHANGELOG.md):
  this task's capability, accurately scoped.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new test above.
- `TestEvaluateApprovalRequiredExampleMatrix` passes for all five
  `ApprovalStatus` values against the documented matrix.
- `TestEvaluateApprovalPolicyAuthorityEventCannotOverridePolicy` and
  `TestEvaluateApprovalFailSafeOnMissingEvidence` both pass — Policy,
  not the event, is authoritative, and missing evidence fails closed.
- `TestEvaluateNoApprovalRuleConfiguredIsUnaffectedByApprovalStatus`
  passes — approval context alone is never an implicit policy.
- `TestAnalyzeApprovalPolicyBehavioralScoreIndependence` passes —
  Anomaly/Trust/Fingerprint are provably independent of
  `ApprovalStatus`.
- `TestAnalyzeApprovalPolicyGenericNotHardCodedToAIAgent` passes — the
  mechanism is domain-generic.
- No new package, no new pipeline stage, no new anomaly signal, no new
  persistence, no new dependency — verified by reviewing the diff
  against this constraint (`git diff -- go.mod go.sum processor/go.mod
  processor/go.sum` empty).
- `BenchmarkEngineAnalyze` and `BenchmarkEvaluateApprovalCondition`
  confirm zero added allocation.
- CLI and `processor/` require zero code changes — both already
  delegate to `config.CompilePolicy`/`internal/policy` — verified
  directly (none were made).
