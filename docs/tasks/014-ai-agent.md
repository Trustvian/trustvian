# 014 — AI Agent Event/Context Foundation

**Milestone:** v0.7 (previously the unnumbered "AI Agent phase" — see
[ROADMAP.md § v0.7](../ROADMAP.md#v07--ai-agent-behavioral-security))
· **Depends on:** `v0.1` shipped (stable `Event` shape — extensions
here must be backward-compatible additions); benefits from, but does
not require, `v0.6`'s sequence-aware signals (proven by reuse, not
extended) · **Blocks:** [015](015-trustvian-mcp.md) (richer agent
context makes the MCP layer more useful, though not strictly required
for it to exist)

## Objective

Extend `event.Event` with optional fields for AI-agent-specific
context — session grouping, agent-to-agent delegation, and a
human-approval status — while proving, not just asserting, that the
*same* pipeline packages (`internal/features` through
`internal/policy`, plus every `v0.6` sequence signal) keep working
unchanged, and that they already detect agent-specific novelty once
agent behavior is represented with the right event shape. See [ADR
0014](../adr/0014-ai-agents-as-first-class-behavioral-actors.md) for
the full design.

## Why

`ActorTypeAIAgent` and `OperationCategoryTool` already exist and are
exercised end-to-end today (see
[use-cases.md § AI-agent security](../use-cases.md#ai-agent-security))
— an agent's tool call already flows through the identical
fingerprint/baseline/anomaly/trust/policy pipeline as any other actor,
with `Actor.Type` and `Operation.Category` as the only differentiators.
The roadmap brief is explicit that this must stay true: "the same
behavioral pipeline should work... Do NOT create a completely separate
security engine for AI agents." What's missing isn't a new pipeline —
it's a few pieces of context (session, delegation, approval state)
that today's generic `Event` shape has no field for, so a producer has
nowhere to put them except an ad hoc `Attributes` key with no defined
meaning. Now that `v0.6` exists, this task also has a mandatory,
much sharper question `v0.6` alone couldn't answer when this task was
first drafted: does the *existing* higher-order sequence detector
(task 027's bounded 3-gram) generalize to AI-agent tool sequences with
zero modification? Proving that (not assuming it) is this task's
central deliverable.

## Scope

```text
Event (unchanged shape, additive fields only)
      ↓
Context gains SessionID, DelegatedFrom, ApprovalStatus
      ↓
features.Extract (unchanged — none of the three new fields are read)
      ↓
Fingerprint / Baseline / Anomaly / Trust / Policy — all unchanged,
proven via integration tests through the real, gated Analyze+Observe
loop, using agent-shaped Events
```

- `event.Context` gains three new optional fields: `SessionID string`,
  `DelegatedFrom string`, `ApprovalStatus ApprovalStatus`.
- `event.ApprovalStatus` — a new exported string-enum type
  (`ApprovalUnspecified` (zero value), `ApprovalNotRequired`,
  `ApprovalRequired`, `ApprovalApproved`, `ApprovalDenied`), following
  `OperationDirection`'s own existing "typed, optional, currently
  unconsumed by `Extract`" precedent exactly.
- Agent-to-agent calls, tool calls, external destinations, file
  access, database access, and secret access are all already
  representable through the generic `Event`/`Actor`/`Target` model
  (`ActorTypeAIAgent`, `OperationCategoryTool`,
  `TargetCategoryExternal`/`Database`) — this task adds session and
  delegation *correlation* fields, not new representational capability
  the pipeline lacks.
- Tool-sequence analysis is not built here — it's `v0.6`'s existing
  `ngram_deviation`/`transition_deviation` signals, applied to
  `Fingerprint.ID`-per-tool-call sequences, proven (not just claimed)
  to work unmodified for agent-shaped events.

## Non-Goals

- **No tool-sequence analysis algorithm.** `v0.6` (tasks 025–028)
  already built this; this task only proves it applies to agent
  events, via `TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine`.
- **No agent-specific `Fingerprint`/`Baseline`/`Anomaly` types.** The
  existing generic types are reused unchanged.
- **No dedicated "agent security engine," module, or package.**
- **No secret-access-specific logic beyond what
  `anomaly.Config.SensitiveTargetFloor` already provides.**
- **No model/provider identity field.** Deliberately excluded — see
  ADR 0014's "Agent identity vs. model identity" section for why
  conflating the two would be a modeling error, not a missing feature.
- **No data-classification taxonomy** (`TargetCategorySensitive`/
  `Secret`/`CustomerData`) — no current consumer needs one beyond what
  `SensitiveTargetFloor` already provides.
- **No approval workflow engine.** `ApprovalStatus` records a fact; it
  is not managed, persisted as pending state, or used to gate
  processing.
- **No delegation graph.** `DelegatedFrom` is a single hop; no
  `DelegationID`, no `DelegationDepth`, no multi-hop chain concept.
- **No raw prompt/completion/tool-argument retention anywhere**, in
  behavioral state or otherwise — see ADR 0014 and
  `docs/SECURITY.md § AI Agent behavioral security`.
- **No OTel GenAI semantic-convention adoption.** `internal/otel` is
  unmodified; potential future mappings are documented, not
  implemented, in `docs/OPENTELEMETRY.md`.
- **No LLM SDK dependency, no embeddings, no vector database, no MCP-specific
  logic.**

## Technical Requirements

- Every new field is optional; `Event.Validate()`'s existing
  required-field set is unchanged — no existing valid `Event` becomes
  invalid, and none of the three new fields is checked by `Validate`
  (matching `TargetCategory`'s own precedent).
- `SessionID`/`DelegatedFrom`/`ApprovalStatus` must not appear in
  `features.StableFeatures` — proven by test, not just by omission,
  at both the `features`/`fingerprint` level and, empirically, at the
  1,000-distinct-session-IDs scale.
- No change to `fingerprint.Compute`'s hash inputs from this task.
- `baseline.Key` is unaffected — session/delegation/approval context
  never influences which `Baseline` an event's learning updates.

## Tests

`event/event_test.go` (+3): `TestEventValidateIgnoresAgentContextFields`,
`TestEventJSONRoundTripAgentContext`,
`TestEventJSONOmitsUnsetAgentContextFields`.

`internal/fingerprint/fingerprint_test.go` (+1):
`TestFingerprintIDIndependentOfAgentContext` — extends the existing
`TestFingerprintIDIndependentOfEventIdentifiers` pattern to
`SessionID`/`DelegatedFrom`/`ApprovalStatus`.

`engine_test.go` (+5):
`TestAnalyzeAgentSessionIDDoesNotExplodeBaseline` (the mandatory
cardinality proof: 1,000 distinct `SessionID` values, otherwise
identical behavior, accumulate into exactly one `Fingerprint` entry
with `Count == 1000`, verified directly against the `Store`),
`TestAnalyzeAgentToolNoveltyDetectedByExistingEngine` (the existing
`categorical_novelty`/`transition_deviation` signals fire on an
unexpected tool call, with no new detector),
**`TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine`** — the
task's own mandatory critical semantic test: `search -> secret.read`
and `secret.read -> external.post` each trained as familiar pairwise
transitions via different contexts, the complete
`search -> secret.read -> external.post` sequence never trained as one
continuous path, and the pre-existing `ngram_deviation` signal (task
027) still detects the higher-order novelty,
`TestAnalyzeAgentCrossActorIsolation`,
`TestAnalyzeAgentDelegationContextScoredIdentically` (proves
`DelegatedFrom` has zero scoring effect today, while remaining present
on the `Event`).

All of the above run under `go test ./... -race -count=1`.

## Benchmarks

Re-ran `BenchmarkExtract`/`BenchmarkCompute` (`internal/features`,
`internal/fingerprint`) — confirmed unchanged (`0 B/op, 0 allocs/op`
and `120 B/op, 15 allocs/op` respectively), since the new `Context`
fields are read by neither function when unset, and the fields
themselves are plain strings/one small enum with no allocation cost of
their own until actually populated by a caller. See
[docs/PERFORMANCE.md § v0.7 task 014](../PERFORMANCE.md#v07-task-014-ai-agent-eventcontext-foundation).

## Documentation

- [docs/adr/0014-ai-agents-as-first-class-behavioral-actors.md](../adr/0014-ai-agents-as-first-class-behavioral-actors.md)
  (new): the full design.
- [docs/DOMAIN.md](../DOMAIN.md): new "AI Agent behavioral context"
  section, explicitly distinguishing fingerprint-affecting fields from
  context-only ones.
- [docs/SECURITY.md](../SECURITY.md): new "AI Agent behavioral
  security" section (session-ID cardinality, prompt/argument privacy,
  agent identity spoofing, delegation abuse, tool abuse, external
  exfiltration, baseline poisoning, rapid tool-call state exhaustion —
  each marked implemented/naturally-supported/future).
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md): `Actor` sources diagram
  extended to show AI agents as another behavioral source, not a
  second pipeline.
- [docs/OPENTELEMETRY.md](../OPENTELEMETRY.md): potential future GenAI
  semantic-convention mappings, documented, not implemented.
- [trustvian-project-spec.md](../../trustvian-project-spec.md): AI
  agents documented as behavioral actors analyzed by the same engine.
- [docs/ROADMAP.md](../ROADMAP.md): `v0.7`'s small-slice sequence
  defined; this task marked done.
- [README.md](../../README.md) / [CHANGELOG.md](../../CHANGELOG.md):
  `v0.7` in-progress status and this task's own capability, accurately
  scoped (no overselling — no tool-sequence *algorithm* was added,
  only proven to already work).

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new test above.
- `TestAnalyzeAgentSessionIDDoesNotExplodeBaseline` and
  `TestFingerprintIDIndependentOfAgentContext` both pass — the
  mandatory proof that `SessionID`/`DelegatedFrom`/`ApprovalStatus`
  never influence `Fingerprint`/`baseline.Key` identity.
- `TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine` passes
  — the mandatory proof that `v0.6`'s existing sequence detection
  generalizes to AI-agent tool sequences with zero new detector code.
- No new package, no new pipeline stage, no agent-specific
  `Fingerprint`/`Baseline`/`Anomaly` type, no new dependency — verified
  by reviewing the diff against this constraint, not just by intent
  (`git diff -- go.mod go.sum processor/go.mod processor/go.sum` empty).
- `Extract`/`Compute` benchmarks unchanged when the new fields are
  unset.
- CLI, `processor/`, and the OTel adapter require zero code changes —
  verified directly (none were made).
