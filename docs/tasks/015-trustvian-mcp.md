# 015 — Trustvian MCP

**Milestone:** Trustvian MCP · **Depends on:** v0.1 shipped (a stable
`Result`/public API to expose); benefits from but does not strictly
require [014](014-ai-agent.md) · **Blocks:** none

## Objective

An MCP server exposing Trustvian's read/query surface to AI agents and
developer tooling, as a new adapter depending on the core — never the
other way around, exactly mirroring how `cmd/trustvian` and
`internal/otel` already relate to `Engine` today.

## Why

AI agents and developer tools increasingly integrate via MCP rather
than a bespoke SDK call; this is a distribution/integration surface,
not new decision-making logic — Trustvian already computes everything
these tools would name (`get_trust_score` ≈ `Result.Trust.Score`,
`explain_decision` ≈ [007](007-decision.md)'s `Result.Explain()`), it
just doesn't expose it via MCP yet.

## Scope

- A new binary (e.g. `cmd/trustvian-mcp`, in-module like
  `cmd/trustvian` today, or a separate module if
  [009](009-otel-collector.md)'s public-API-boundary question resolves
  in a direction that makes a separate module cleaner — decide
  consistently with that precedent when this task starts) implementing
  an MCP server backed by one `Engine` instance.
- Tools, mapped directly onto existing capabilities — no new
  computation:
  - `get_behavior` → `Engine.Analyze` (read-only; does not call
    `Observe`, matching `Analyze`'s existing read-only contract).
  - `get_trust_score` / `get_risk` / `get_anomaly` → fields off the
    `Result` a prior `get_behavior` (or an internal equivalent) call
    produced.
  - `explain_decision` → [007](007-decision.md)'s `Result.Explain()`.
  - `evaluate_policy` → `Policy.Evaluate` against a supplied
    `Trust`/`Features.Stable` — bounded by whatever
    [ADR 0002](../adr/0002-public-api-boundary.md) resolution
    [009](009-otel-collector.md) settles on for external `Policy`
    construction; if `Policy` is still internal-only when this task
    starts, `evaluate_policy` operates against the server's own
    configured policy, not an arbitrary caller-supplied one.
  - `check_tool_call` → a thin, agent-oriented wrapper over
    `get_behavior` for the specific case of an agent checking a
    proposed tool call *before* making it (a read-only "would this be
    flagged" query, using the same `Analyze` path — no new pipeline
    behavior, just a purpose-named entry point).

## Non-Goals

- **No new decision-making logic in the MCP layer.** Every tool is a
  thin wrapper over `Engine`'s existing public methods — this task
  must not become a second policy engine or a second scoring path.
- **No MCP dependency in the core.** `event`, `internal/*`, and the
  root `trustvian` package must never import an MCP library — verified
  the same way [008](008-otel.md) verified no cycle: `go list -deps`.
- No write/mutating MCP tools beyond what `Observe` already is
  (read-only `get_*`/`explain_*`/`check_*` tools only, unless a
  concrete need for an explicit `observe` tool emerges — decide
  narrowly, don't default to exposing every `Engine` method just
  because it exists).
- No agent authentication/authorization layer — MCP transport-level
  auth is the deploying application's concern, exactly like
  [SECURITY.md § telemetry spoofing](../SECURITY.md#telemetry-spoofing)
  already establishes for `IdentityConfidence` more generally.

## Technical Requirements

- The MCP binary/module depends on the root `trustvian` package (and
  `event`) only — same public-API-only constraint every other adapter
  already respects.
- Each tool's input/output schema is explicit and versioned (MCP's own
  schema mechanism) — not a loose, ad hoc JSON shape.

## Tests

- Per-tool tests exercising the MCP server against a real `Engine`
  (constructed via the public API, exactly like every SDK-level test
  in this repository already does) — no mocking of `Engine` itself.
- A test confirming `get_behavior` never mutates the underlying
  `Store` (mirrors `TestAnalyzeIsReadOnly` in `engine_test.go`).

## Benchmarks

- Not expected to be a high-throughput hot path (interactive
  agent/developer queries, not per-request production traffic) — no
  benchmark required unless a concrete latency concern emerges during
  implementation.

## Documentation

- New `docs/MCP.md` (or a section in an existing doc — decide during
  implementation based on how large this ends up being): tool
  reference, example agent interaction.
- [ARCHITECTURE.md](../ARCHITECTURE.md): add the MCP adapter to the
  system diagram once it exists, alongside `cmd/trustvian` and
  `internal/otel`.

## Acceptance Criteria

- `go list -deps` (or the MCP module's own equivalent) confirms zero
  MCP dependency anywhere under `event`/`internal/*`/the root package.
- Every tool is demonstrably a thin wrapper (no new scoring/decision
  logic) — verified by code review against this specific constraint.
- Read-only tools proven not to mutate state, by test.
