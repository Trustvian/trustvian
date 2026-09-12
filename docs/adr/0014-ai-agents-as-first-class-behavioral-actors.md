# 0014 — AI agents as first-class behavioral actors, not a second engine

## Context

[Task 014](../tasks/014-ai-agent.md) opens `v0.7` — AI Agent Behavioral
Security. `ActorTypeAIAgent` and `OperationCategoryTool` already
existed (`v0.1`) and already flow through the full, unmodified
pipeline (`event.Extract` → `fingerprint.Compute` → `baseline.Observe`
→ `anomaly.Score` → `trust.Compute` → `policy.Evaluate`). What's
missing is not detection capability — it's a place to put a handful of
correlation facts an AI agent's behavior genuinely needs (which
conversation this event belongs to, who delegated it, whether it was
approved) that today's generic `Event` shape has no field for.

## Decision: extend `Event`, do not build a second engine

AI agents are represented as another `Actor` flowing through the
*same* `Engine`. No `AgentEngine`, `AgentRiskEngine`, or
`AgentAnomalyEngine` exists or is created. Concretely, this task adds:

- `event.Context` gains three optional fields: `SessionID`,
  `DelegatedFrom`, `ApprovalStatus` (a new, small string-enum type).
- `event.ApprovalStatus` — a new exported type, five values including
  the zero value (`ApprovalUnspecified`, `ApprovalNotRequired`,
  `ApprovalRequired`, `ApprovalApproved`, `ApprovalDenied`).
- Nothing else. No new package, no new pipeline stage, no agent-specific
  `Fingerprint`/`Baseline`/`Anomaly` type.

This mirrors the identical "extend, don't parallel" decision this
codebase already made for `event.Actor`/`Target`/`Operation`
themselves — the same reasoning ADR 0001 applied to the pipeline shape
applies here to the domain model that feeds it.

## Agent identity vs. model identity — kept explicitly separate

`Actor.ID` (e.g. `"customer-support-agent-42"`) is the agent's
behavioral identity — the dimension `baseline.Key`/`Fingerprint`
already key on. A model or provider name (e.g. `"gpt-5"`,
`"claude-opus-5"`) is *not* behavioral identity: two different agents
built on the same model are different actors with different baselines,
and the same agent migrating between model versions is still the same
actor. This task deliberately adds **no field for model/provider
metadata** — there is no current consumer that would read it, and
`Event.Attributes` already provides a place for a producer to attach
it for observability (exactly like `duration_ms`) without it being
mistaken for identity. Adding a typed field with no reader would be
exactly the speculative modeling CLAUDE.md and this task's own brief
warn against.

## What affects `Fingerprint` identity, and what deliberately does not

This is the single most important design question this task answers,
stated explicitly rather than left implicit:

```
Affects Fingerprint (StableFeatures):
  Actor.Type, Operation.Category, Operation.Name,
  Target.Name, Target.Category, Context.Environment
  (all pre-existing — unchanged by this task)

Does NOT affect Fingerprint — correlation/context only:
  Context.TraceID, Context.SpanID       (pre-existing)
  Context.SessionID                      (new, this task)
  Context.DelegatedFrom                  (new, this task)
  Context.ApprovalStatus                 (new, this task)
```

**Why `SessionID` must never enter `Fingerprint`/`baseline.Key`.** A
session identifier is, by construction, typically unique per
conversation — an agent that handles a thousand customer conversations
has a thousand distinct `SessionID` values but is still *one*
behavioral actor whose tool-use pattern should be learned across all
of them. Folding `SessionID` into `Fingerprint` (or, worse, into
`baseline.Key`) would give every session a fresh, never-reused
Fingerprint or Baseline — the system would never accumulate enough
observations to learn *anything*, defeating the entire purpose of
behavioral profiling. Proven, not just asserted:
`TestAnalyzeAgentSessionIDDoesNotExplodeBaseline` runs 1,000 events
with 1,000 distinct `SessionID` values and otherwise-identical
behavior, and confirms exactly one `Fingerprint` entry accumulates a
`Count` of 1,000 — not 1,000 separate entries.

**Why `DelegatedFrom` and `ApprovalStatus` also stay off the
Fingerprint.** Both are per-event operational facts about *this
specific action* (who asked for it, whether it was approved) — not a
stable dimension of *what this actor typically does*. Treating them as
identity would, at best, add noise to the Fingerprint's cardinality for
no behavioral benefit, and at worst (for `DelegatedFrom`, which can
vary per call like a session ID) risk the identical
baseline-fragmentation problem `SessionID` has.

## Tool calls and destinations reuse existing types, add none

An AI-agent tool call is represented with the existing `Operation`/
`Target` types exactly as any other actor's operation would be:
`Operation{Category: OperationCategoryTool, Name: "shell.execute"}`,
`Target{Name: "...", Category: TargetCategoryExternal}` for an outbound
destination. No `Tool`, `ToolCall`, or `AgentDestination` type is
added — `OperationCategoryTool`, `TargetCategoryExternal/Database`,
and `Config.SensitiveTargetFloor` already existed and already cover
every representational need this task identified. A genuine
data-classification system (a `TargetCategorySensitive`/`Secret`/
`CustomerData` taxonomy) was considered and explicitly deferred — no
current consumer needs it, and `SensitiveTargetFloor`'s existing
name-keyed mechanism already lets an operator mark specific sensitive
destinations regardless of category.

## Tool identity, tool arguments, and privacy — a hard rule, not a suggestion

Tool identity (`Operation.Name`, `Target.Name`) must be a stable,
low-cardinality string (`"filesystem.read"`, `"database.query"`,
`"secret.read"`) — never raw prompt text, tool argument values, or
free-form user input. This is not merely a style preference: an
argument value can contain PII, secrets, database contents, or
credential-bearing URLs, and putting it in `Operation.Name`/
`Target.Name` would both (a) explode Fingerprint cardinality (every
distinct argument value becomes a "new" behavioral identity) and (b)
retain sensitive data inside behavioral state indefinitely, with no
existing Trustvian mechanism to redact or expire it. Trustvian's
existing data model has no field for raw tool arguments, prompt text,
or completion text, and this task adds none — this is `SECURITY.md`'s
own existing "no raw payload in fingerprint identity" principle,
restated for AI-agent tool arguments specifically. See
`docs/SECURITY.md § AI Agent behavioral security` for the full threat
writeup.

## No new detector — the existing engine already works, proven not asserted

This task adds **zero** new anomaly signals. Instead, it proves — with
integration tests through the real `Engine.Analyze`/`Observe` loop,
not synthetic fixtures — that the existing signals already work
correctly once agent behavior is represented with the right event
shape:

- `TestAnalyzeAgentToolNoveltyDetectedByExistingEngine`: an agent that
  has only ever used `search`/`read`/`summarize`, then calls
  `shell.execute`, is flagged by the pre-existing
  `categorical_novelty`/`transition_deviation` signals (tasks
  004/025) — no agent-specific detector exists.
- `TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine`: the
  task's own mandatory critical semantic test, mirroring [task
  027](../tasks/027-bounded-ngram-detection.md)'s own proof exactly.
  `search -> secret.read` and `secret.read -> external.post` are each
  trained as familiar pairwise transitions (via different contexts);
  the complete sequence `search -> secret.read -> external.post` is
  never trained as one continuous path. The pre-existing
  `ngram_deviation` signal (task 027) still detects the higher-order
  novelty — direct proof that `v0.6`'s bounded 3-gram detection, built
  with zero knowledge of AI agents, already generalizes to agent tool
  sequences without modification.

## Human approval: a recorded fact, not a workflow engine

`ApprovalStatus` records what happened to *this* event (was approval
required, was it granted) — Trustvian does not manage an approval
process, does not persist pending-approval state, and does not gate or
delay an `Event`'s processing on it. This is the identical
"typed but currently unconsumed" precedent `OperationDirection`
already established in this codebase: the field exists, is validated
as a type, and is available for a *future* task to wire into a signal
or a `Policy` condition, once one has a concrete design — building that
consumer speculatively, in this task, would be exactly the workflow
engine the brief explicitly says not to build.

## Delegation: one hop, no graph

`DelegatedFrom` carries the immediate parent `Actor.ID` for a single
agent-to-agent delegation hop. A full delegation *graph* (multi-hop
chains, `DelegationID`, `DelegationDepth`) was considered and rejected
for this task — no concrete consumer needs more than "who asked for
this," and a graph model is real, non-trivial future work (agent
relationship/graph analytics), explicitly out of this task's scope.
`TestAnalyzeAgentDelegationContextScoredIdentically` proves
`DelegatedFrom` has no scoring effect today (two otherwise-identical
events, differing only in `DelegatedFrom`, produce byte-for-byte
identical `Anomaly`/`Trust`/`Decision`) while still being present on
the `Event` for a later task to read.

## OpenTelemetry: adapter concern, not a core dependency

`internal/otel` remains the only package depending on OpenTelemetry.
This task does not modify `internal/otel.EventFromSpan` — OTel's own
GenAI semantic conventions (`gen_ai.*` attributes) are still evolving
and explicitly not stable enough to hard-code into this module's
public domain model. `docs/OPENTELEMETRY.md` documents *potential*
future mappings (e.g. a `gen_ai.conversation.id` span attribute could
populate `Context.SessionID`) as adapter-layer guidance, not as
implemented behavior.

## Consequences

- Three new optional `Context` fields, one new small enum type. No new
  package, no new pipeline stage, no new persistence technology, no new
  dependency.
- `features.Extract` is unchanged — it does not read any of the three
  new fields, proven by `TestFingerprintIDIndependentOfAgentContext`.
- `Event.Validate()` is unchanged — the new fields are never checked,
  matching `TargetCategory`'s own existing "typed but unchecked"
  precedent.
- Existing `v0.1`–`v0.6` callers, CLI, and the OTel Collector processor
  see byte-for-byte unchanged behavior: the new fields are purely
  additive JSON fields that `encoding/json` and every existing
  consumer already handle automatically.
