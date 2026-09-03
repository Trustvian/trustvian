# 010 — Real-World Examples

**Milestone:** v0.1 · **Depends on:** none (uses only what's already
implemented; [004](004-anomaly.md)'s frequency signal strengthens
Example 5 but isn't required to ship the others) · **Blocks:**
[013](013-oss-v01.md)

## Objective

A runnable `examples/` directory — real Go programs, not just
documentation prose — covering the six scenarios named in the roadmap
brief, each demonstrating the full
`Event → Fingerprint → Baseline → Anomaly → Trust → Policy → Decision`
path.

## Why

[docs/use-cases.md](../use-cases.md) already demonstrates five of
these six scenarios (missing: abnormal request frequency, which needs
[004](004-anomaly.md)) as verified CLI input/output pairs — but as
*documentation*, not as something a developer can `go run` directly.
The roadmap brief is explicit that runnable examples are "extremely
important for OSS adoption" — this task is the mechanical step of
turning already-proven scenarios into a real `examples/` tree, plus
adding the one genuinely new scenario (frequency).

## Scope

`examples/` (mirroring the original spec's `examples/{basic,otel,...}`
shape, adapted to what actually exists):

1. `examples/basic/` — Example 1 (normal API behavior): construct an
   `Engine`, analyze a well-formed event, print the result. The "hello
   world" of the SDK.
2. `examples/credential-misuse/` — Example 2 (valid identity, abnormal
   behavior): reuses the exact scenario already verified in
   [use-cases.md § valid identity, abnormal behavior](../use-cases.md#valid-identity-abnormal-behavior).
3. `examples/unexpected-dependency/` — Example 3: reuses
   [use-cases.md § API behavioral anomaly](../use-cases.md#api-behavioral-anomaly).
4. `examples/external-destination/` — Example 4: reuses
   [use-cases.md § service-to-service security](../use-cases.md#service-to-service-security).
5. `examples/frequency-abuse/` — Example 5 (abnormal request
   frequency): **new**, depends on [004](004-anomaly.md)'s
   `frequency_deviation` signal actually existing to demonstrate.
6. `examples/ai-agent/` — Example 6: reuses
   [use-cases.md § AI-agent security](../use-cases.md#ai-agent-security).

Each example is a small, self-contained `package main` (a genuine
external-module-style program, following the exact pattern already
verified in [Go SDK Guide § watching trust mature](../sdk-guide.md#watching-trust-mature)
— compiled and run for real, output captured verbatim, not
hand-written) plus a short `README.md` explaining the scenario.

## Non-Goals

- No example requiring anything not yet shipped except Example 5's
  dependency on [004](004-anomaly.md) — every other example must run
  against `v0.1` as it stands today.
- No OTel-sourced examples yet (`examples/otel/` from the original
  spec's sketch) — that's naturally [008](008-otel.md)/[009](009-otel-collector.md)
  territory once outbound attributes exist to make an OTel example
  demonstrate the full round trip meaningfully.
- No example demonstrating policy customization via `WithPolicy` with
  a hand-built `Policy` from *outside* this module — per
  [ADR 0002](../adr/0002-public-api-boundary.md), that's not possible
  from a true external module yet; examples that need a custom policy
  either live in-module (like `cmd/trustvian` does) or use the default
  policy, and must say so explicitly rather than silently working
  around the limitation.

## Technical Requirements

- Each example must actually compile and run as a genuinely external
  consumer of `github.com/Trustvian/trustvian` (verified via `go mod
  replace` during development, exactly as done throughout this
  documentation set) — not merely live inside this module's own
  `internal/` tree.
- No example may require network access, external services, or manual
  setup beyond `go run`.

## Tests

- A `TestExamplesRun` (or a Makefile target, decide during
  implementation) that actually executes each example and checks for a
  zero exit code — examples going stale (failing to build against a
  later `Engine` change) is exactly the kind of drift this task exists
  to prevent from being invisible.

## Benchmarks

None — examples are documentation-adjacent, not hot-path code.

## Documentation

- `examples/README.md`: an index, mirroring [docs/README.md](../README.md)'s
  style.
- [README.md](../../README.md): add an `examples/` pointer once it
  exists (currently absent since the directory doesn't exist).
- [ROADMAP.md](../ROADMAP.md): mark examples implemented.

## Acceptance Criteria

- All six examples build and run successfully via `go run` (or `make`
  targets) with real, captured output.
- Example 5 demonstrably shows `frequency_deviation` in its
  `Contributors` (not just novelty).
- A CI-runnable check (even if CI itself is out of this task's scope,
  the *check* should exist and be documented as something CI should
  run once it exists) exercises every example.
