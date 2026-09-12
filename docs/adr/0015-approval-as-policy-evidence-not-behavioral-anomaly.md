# 0015 — Approval as policy evidence, not behavioral anomaly

## Context

[Task 014](../tasks/014-ai-agent.md) added `event.ApprovalStatus` as a
typed, optional `Context` field, deliberately unconsumed — a per-event
fact recorded, not enforced. [Task
030](../tasks/030-approval-aware-policy-semantics.md) gives it its
first consumer. Before writing any code, this task's own brief posed
the central design question directly: is approval primarily
behavioral evidence (an `anomaly` signal — "is this unusual?"),
deterministic policy context (a `policy` concern — "is this
allowed?"), or both? Getting this wrong would either dilute a
security-relevant deterministic requirement into a learned, gradually
adjusting score, or duplicate one concept across two pipeline stages
that CLAUDE.md's dependency-direction rule already keeps separate.

## Decision: approval belongs to Policy, not Anomaly

`shell.execute` can be completely familiar to an actor's `Baseline` —
low `Anomaly.Score`, low `Trust` risk — and still require approval it
does not have. "Is this unusual?" and "is this allowed?" are different
questions with different answer shapes: the first is a continuous,
learned score that improves with more history; the second is a
deterministic yes/no that a security-sensitive operation needs
regardless of how much history exists. Task 030 adds **zero** new
anomaly signals — no `approval_deviation`, `approval_anomaly`,
`approval_rarity`, `approval_baseline`, or `approval_transition` — and
instead extends `internal/policy` exactly the way task 014's own brief
anticipated: reuse the existing engine, do not build a parallel one.

Concretely:

- `policy.Input` gains one field: `ApprovalStatus event.ApprovalStatus`,
  populated in `Engine.Analyze` from `ev.Context.ApprovalStatus` —
  the same place `Attributes` was already threaded through for
  `Condition.Attributes` matching.
- `policy.Condition` gains one field: `ApprovalStatus
  event.ApprovalStatus`, an equality match identical in kind to every
  other `Condition` field (`ActorType`, `TargetName`, ...). No new
  `Rule`-level primitive, no `RequiresApproval bool`, no special-cased
  evaluation path — `Condition.Matches` gained one more `if` alongside
  the ones already there.
- `config.PolicyCondition` gains the matching `ApprovalStatus string`
  field (`yaml:"approval_status"`), validated against the same four
  non-empty `event.ApprovalStatus` constants, following
  `validActorTypes`'s existing pattern in `config/validate.go`.

Nothing in `internal/anomaly`, `internal/baseline`, or
`internal/trust` changed. `AnomalyConfig`/`TrustConfig` are untouched.

## Policy owns the requirement; the event supplies only evidence

This is the task's second, equally important design question (§9/§10
of the brief): does the *event* declare "I require approval," or does
*Policy* declare "this operation requires approval"? The former would
let the evaluated actor define its own security requirement — an
agent could self-declare `ApprovalNotRequired` and walk past a rule
meant to gate it. Task 030 rejects that model entirely: **whether an
operation requires approval is expressed by which `Rule` exists in
`Policy`, never by the event's own `ApprovalStatus` value.**

The mechanism that makes this concrete is the existing `Unless`
field, applied to `ApprovalStatus` for the first time:

```go
policy.Rule{
	Name:   "shell-execute-requires-approval",
	When:   policy.Condition{OperationCategory: event.OperationCategoryTool, TargetName: "shell.execute"},
	Unless: &policy.Condition{ApprovalStatus: event.ApprovalApproved},
	Action: policy.DecisionBlock,
	Reason: "shell.execute requires approval; approval evidence was not Approved",
}
```

`When` names the operation Policy cares about; `Unless` names the
*one* value (`Approved`) that satisfies the requirement. Every other
`ApprovalStatus` — `Denied`, `Required`, `Unspecified`, and,
critically, `NotRequired` — fails to satisfy `Unless`, so the rule
still fires. An event claiming `ApprovalNotRequired` has exactly as
much power to exempt itself as an event claiming nothing at all: none.
This is the Approval Matrix, proven by
`TestEvaluateApprovalRequiredExampleMatrix`,
`TestEvaluateApprovalPolicyAuthorityEventCannotOverridePolicy`, and
`TestEvaluateApprovalFailSafeOnMissingEvidence` in
`internal/policy/policy_test.go`:

| Event `ApprovalStatus` | Policy requires approval | Requirement satisfied |
|---|---|---|
| `Approved` | Yes | **Yes** — rule suppressed |
| `Denied` | Yes | No — `BLOCK` |
| `Required` | Yes | No — `BLOCK` (functionally pending) |
| `Unspecified` | Yes | No — `BLOCK` (fail-closed on missing evidence) |
| `NotRequired` | Yes | No — `BLOCK` (the event cannot override Policy) |

And, per CLAUDE.md's fail-closed principle applied here: a `Policy`
with **no** approval-aware rule at all is completely unaffected by
`ApprovalStatus` — `TestEvaluateNoApprovalRuleConfiguredIsUnaffectedByApprovalStatus`
proves approval context never becomes an implicit, global policy on
its own. Task 030 is purely additive.

## `ApprovalStatus` remains untrusted, self-reported evidence

Task 014 already established this for `DelegatedFrom`
(`docs/SECURITY.md`'s "Delegation abuse" entry); task 030 extends the
identical boundary to `ApprovalStatus`, now that it has a real
consumer for the first time. `ApprovalStatus = Approved` is *evidence
Trustvian evaluates*, not *proof Trustvian verifies*. Nothing in this
task adds signature verification, an OAuth/IAM check, or any call to
an external authorization system — doing so now would be exactly the
premature cryptographic-verification machinery the task's own brief
warned against building. The integration boundary (whatever produces
the `Event`) is responsible for populating `ApprovalStatus` from a
source it trusts; Trustvian's contract is to evaluate that value
deterministically and explainably, not to establish where it came
from. This is the same principle
`.claude/rules/security.md` § "Identity is an input, not a
computation" already applies to `Actor.IdentityConfidence`, extended
here to approval evidence — see `docs/SECURITY.md` § AI Agent
behavioral security's "Approval self-assertion" entry for the full
threat writeup, now updated to reference this mechanism directly.

`Actor.IdentityConfidence` and `ApprovalStatus` remain deliberately
independent (§28 of the brief): the former is confidence in *who*
performed the operation; the latter is evidence about whether *this
specific operation* was authorized. Coupling them (e.g., requiring
`IdentityConfidence` above some threshold before an approval rule can
be satisfied) was considered and rejected — no concrete requirement
motivates it, and it would conflate two genuinely different signals
this codebase already keeps apart everywhere else (`trust.Compute`
already combines them additively, in `TrustScore`, not by making one
gate the other).

## Approval evidence is per-event, not learned or persisted

No new mutable state was added. `ApprovalStatus` is read once per
`Analyze` call, exactly like `Attributes` already is, and never
written into `Baseline`, `PredecessorCounts`, `TrigramCounts`, or any
Markov statistic — `TestAnalyzeApprovalPolicyBehavioralScoreIndependence`
proves this directly: two events identical except for `ApprovalStatus`
produce byte-for-byte identical `Anomaly.Score`, `Trust.Score`, and
`Fingerprint.ID`, differing only in the final `Decision`. There is no
`ApprovalStore`, `ApprovalRepository`, `PendingApproval`, or
`ApprovalHistory` — Trustvian evaluates the event in front of it; it
does not track approval state across events or sessions. Session
scoping (`Context.SessionID`, task 014) was explicitly not extended
into an "approval persists for this session" cache — every approval
decision is re-evaluated per event, keeping the model stateless and
deterministic, per the brief's own §30/§31.

## Decision, Alert, and CLI/processor integration are unchanged

An approval-gated `BLOCK` is a `policy.Result` exactly like any other
— `Explanation.RuleName`/`Reason` name the rule that fired, and
nothing new was added to `Explanation` (the brief's own §17 asked for
"which policy matched, observed approval state, resulting action";
`RuleName`/`Reason` plus the caller's own access to
`Result.Event.Context.ApprovalStatus` already cover this — no dynamic
interpolation was added to `Explanation`, matching every other
`Condition` field's existing static-`Reason` convention). `alert.Rule`/
`alert.Condition` are untouched: `alert.Condition{Decision:
policy.DecisionBlock}` already matches an approval-triggered block
exactly like any other block, since `alert.Evaluate` reads
`Result.Decision`, not how that `Decision` was produced. No
`ApprovalAlertEngine` was built or is needed. The CLI and the OTel
Collector processor both already delegate policy evaluation to
`config.CompilePolicy`/`internal/policy` — an approval-aware `Policy`
compiled from YAML works through the identical `trustvian analyze
--config`/Collector path with zero code changes on either side.

## Consequences

- One new field each on `policy.Input`, `policy.Condition`, and
  `config.PolicyCondition`. No new package, no new pipeline stage, no
  new persistence, no new dependency.
- `internal/anomaly`, `internal/baseline`, `internal/trust` are
  unchanged — approval enforcement is provably independent of
  behavioral scoring.
- Existing policies with no approval-aware rule are byte-for-byte
  unaffected, proven by test, not just by omission.
- `ApprovalStatus` still carries no verified provenance; a future task
  that needs verified approval (e.g., a signed assertion from a
  specific authorization system) is a distinct, larger piece of work
  this task deliberately does not build ahead of a concrete need.
