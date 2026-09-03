# 014 — AI Agent Security: Extend the Event Model

**Milestone:** AI Agent phase · **Depends on:** v0.1 shipped (stable
`Event` shape — extensions here must be backward-compatible additions)
· **Blocks:** [015](015-trustvian-mcp.md) (richer agent context makes
the MCP layer more useful, though not strictly required for it to
exist)

## Objective

Extend `event.Event` with optional fields for AI-agent-specific
context — session grouping, agent-to-agent delegation, and
human-approval workflow state — while proving, not just asserting,
that the *same* pipeline packages (`internal/features` through
`internal/policy`) keep working unchanged.

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
meaning.

## Scope

- `Context` (or a new optional sub-struct) gains a `SessionID` field —
  groups events belonging to one agent conversation/session, without
  changing `Fingerprint`'s composition (session ID must **not** enter
  `StableFeatures`/`Fingerprint` — it's closer to volatile/correlation
  data like `TraceID`, not a behavioral-identity dimension; verify this
  explicitly, mirroring [001](001-feature-model.md)'s trace-ID
  regression test).
- `Actor` (or `Context`) gains an optional `DelegatedFrom` field
  (another `Actor.ID`) for agent-to-agent delegation chains — a single
  hop is sufficient scope; a full delegation *graph* concept is not
  justified without a concrete consumer.
- Document the human-approval *workflow* pattern as a convention over
  existing primitives: `REQUIRE_APPROVAL` (already a `Decision` value)
  plus a documented `Attributes` key (or [006](006-policy.md)'s
  attribute-matching `Condition`, if it lands first) for recording an
  approval's outcome on a follow-up event — no new `Decision` value,
  no new pipeline stage.

## Non-Goals

- **No tool-sequence analysis.** This is a new anomaly *algorithm*
  (order-aware, not just new fields), explicitly out of scope per
  [ADR 0001](../adr/0001-hexagonal-core-and-pipeline-shape.md)'s "add a
  second algorithm only when it has a concrete design" — belongs in
  `v0.3`+/[Future research](../ROADMAP.md#future-research), not here.
- **No agent-specific `Fingerprint`/`Baseline`/`Anomaly` types.** The
  existing generic types must be reused unchanged — this task adds
  optional `Event` fields, not new domain types.
- No dedicated "agent security engine," module, or package — matches
  the roadmap brief's explicit instruction.
- No secret-access-specific logic beyond what
  `anomaly.Config.SensitiveTargetFloor` already provides — an agent
  accessing a secrets manager already gets the same floor treatment
  any actor does; no agent-specific carve-out is needed or wanted.

## Technical Requirements

- Every new field is optional; `Event.Validate()`'s existing
  required-field set is unchanged — no existing valid `Event` becomes
  invalid.
- `SessionID`/`DelegatedFrom` must not appear in
  `features.StableFeatures` — proven by test, not just by omission.
- No change to `fingerprint.Compute`'s hash inputs from this task
  (distinct from [001](001-feature-model.md)'s `TargetCategory`, which
  *does* change them) — reinforces that session/delegation are
  correlation data, not behavioral identity.

## Tests

- `event/event_test.go`: new optional fields don't break `Validate()`
  for existing valid events; explicit test that `SessionID` doesn't
  leak into `Fingerprint.ID` (mirrors [001](001-feature-model.md)'s
  trace-ID regression test pattern).
- An end-to-end test (in `engine_test.go`, following existing
  patterns) of a multi-event agent session: same `SessionID` across
  several tool calls, proving the existing baseline-maturity mechanics
  ([Go SDK Guide § watching trust mature](../sdk-guide.md#watching-trust-mature))
  work identically for session-grouped agent events as for any other
  actor — this is the direct proof that "no separate engine" held.
- A delegation-chain test: an event with `DelegatedFrom` set is scored
  by the exact same pipeline, with the delegation fact available for a
  policy to match on ([006](006-policy.md)'s attribute matching, or a
  dedicated `Condition` field if that proves cleaner during
  implementation).

## Benchmarks

- Re-run `BenchmarkExtract`/`BenchmarkCompute` (features, fingerprint)
  to confirm the new optional fields don't regress the zero-allocation
  common case when unset.

## Documentation

- [DOMAIN.md § Event](../DOMAIN.md#event): document the new fields and
  explicitly restate why they're correlation data, not stable
  features.
- [use-cases.md](../use-cases.md): extend the existing AI-agent
  scenario to show session grouping in action.
- [ROADMAP.md](../ROADMAP.md): mark this phase's scope implemented;
  keep tool-sequence analysis clearly in Future research.

## Acceptance Criteria

- `go test ./... -race` green, including new session/delegation tests.
- Explicit, passing test proving `SessionID`/`DelegatedFrom` never
  influence `Fingerprint.ID`.
- No new package, no new pipeline stage, no agent-specific
  `Fingerprint`/`Baseline`/`Anomaly` type — verified by reviewing the
  diff against this constraint, not just by intent.
- Zero-allocation benchmarks for `Extract`/`Compute` unchanged when
  the new fields are unset.
