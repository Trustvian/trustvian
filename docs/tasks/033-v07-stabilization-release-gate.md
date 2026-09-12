# 033 — v0.7 Stabilization & Release Gate

**Milestone:** v0.7 · **Depends on:**
[014](014-ai-agent.md)–[032](032-agent-security-scenario-validation.md)
· **Blocks:** `v0.7.0` (tag it only once this gate passes) ·
**Mirrors:** [024](024-v05-release-gate.md)/
[029](029-v06-stabilization-release-gate.md) — same shape, applied to
`v0.7`, with one added dimension neither predecessor had: task 032
surfaced a genuine release blocker (a public-API gap) mid-audit, so
this gate's scope grew to include fixing it, not just verifying
014–032's own claims.

## Objective

Not new detection capability — a verification, blocker-remediation, and
documentation-synchronization pass tying tasks 014–032 together into a
release-ready `v0.7.0` candidate. Feature freeze was in effect for the
entire pass: no new anomaly detector, no new AI-agent feature, no
`v0.8` work.

## Why

Task 032's own scenario-validation pass, while building the one
fully-public example its own brief required, found that
`trustvian.WithAnomalyConfig` takes an `internal/anomaly.Config`
value — a type no external module can construct, by any means, since
no public function anywhere returned one for pass-through (unlike
`policy.Policy`, which `config.CompilePolicy` already solved this for
in `v0.5`). This is a genuine release blocker under this project's own
release principle: a capability that is implemented, tested, and
documented but not externally enable-able is not release-complete for
an OSS product. `v0.5`'s and `v0.6`'s own stabilization gates
(024/029) each closed out a milestone that had no comparable
blocker — this is the first `v0.7`-specific gate that had one to fix,
not just verify.

## Scope

```text
Part 1 — Close the public anomaly configuration gap
  config.AnomalyConfig (new public document)
        ↓
  config.CompileAnomaly (new compiler, mirrors CompilePolicy exactly)
        ↓
  anomaly.Config (unchanged internal type)
        ↓
  trustvian.WithAnomalyConfig (unchanged signature)

Part 2 — Full v0.7 stabilization
  Re-verify 014/030/031/032 against current source (not memory)
        ↓
  External-consumer proof, CLI parity, documentation audit
        ↓
  Release-readiness decision
```

- `config/anomaly.go` (new): `AnomalyConfig`, mirroring every field
  `internal/anomaly.Config` already has.
- `config/compile.go` (+`CompileAnomaly`): validates and translates
  `AnomalyConfig` into `anomaly.Config`, starting from
  `anomaly.DefaultConfig()` so an unset field always resolves to the
  existing default.
- `config/validate.go` (+`AnomalyConfig.Validate`, two new sentinels:
  `ErrUnsupportedAnomalyVersion`, `ErrInvalidZThreshold`).
- `config/load.go` (+`LoadAnomaly`/`LoadAnomalyFile`): the third
  independent document/loader pair, alongside `Load`/`LoadFile`
  (Policy) and `LoadAlerts`/`LoadAlertsFile` (Alert).
- `cmd/trustvian` (+`--anomaly-config <path>` on both `analyze` and
  `baseline build`): the CLI's existing `newEngine` helper gains a
  second config path, applying `trustvian.WithAnomalyConfig` when set,
  unchanged when not.
- `examples/ai-agent-security` (rewritten): now demonstrates the full
  combined scenario (delegation + sequence + approval) through public
  config alone, plus a `main_test.go` proving it from within a
  genuinely separate Go module.
- No changes to `internal/anomaly`, `internal/baseline`,
  `internal/policy`, `internal/trust`, `alert`, `event`, `engine.go`,
  or `options.go`.

## Non-Goals

- **No new anomaly detector, no new AI-agent feature, no `v0.8` work.**
  This task closes a configuration-boundary gap and stabilizes; it
  does not extend behavioral capability.
- **No processor changes.** `processor/go.mod` still pins `v0.5.0`,
  predating every `v0.6`/`v0.7` capability equally — this task's new
  surface is not yet reachable from the processor, and fixing that is
  a distinct future task (bumping the pinned dependency), not this
  one's.
- **No `internal/baseline` state-bound configuration** (`maxPredecessors`/
  `maxTrigramPredecessors`/`maxDelegators`). These stay hardcoded,
  deliberately — see ADR 0017's own reasoning.
- **No combined `Config` document merging Policy/Alert/Anomaly.** Each
  stays its own independent document, matching ADR 0009's existing
  precedent for Alert vs. Policy, extended identically to Anomaly.
- **No `v0.7.0` tag, no release.**

## Test Plan

`config/anomaly_test.go`, `config/anomaly_compile_test.go`,
`config/anomaly_load_test.go`, `config/anomaly_bench_test.go` (new):
validation (every weight field, both plain and pointer; z-thresholds;
`SensitiveTargetFloor` values; the mandatory negative-into-`uint64`
rejection proof), the mandatory
**`TestCompileAnomalyZeroValueMatchesDefaultConfig`** backward-compatibility
proof, determinism/no-mutation, strict-decoding parity with
Load/LoadAlerts, and compile-cost benchmarks.

`cmd/trustvian/main_test.go` (+4): `TestRunAnalyzeAcceptsAnomalyConfigFlag`,
`TestRunAnalyzeInvalidAnomalyConfigFailsClosed`,
`TestRunAnalyzeMissingAnomalyConfigFailsClosed`,
`TestRunBaselineBuildAcceptsAnomalyConfigFlag`.

`examples/ai-agent-security/main_test.go` (new, run from within the
genuinely separate `examples` Go module — the mandatory external-consumer
proof): **`TestPublicConfigConfiguresDelegationAndNGramSignals`** (public
config → `Engine` → real `delegation_deviation`/`ngram_deviation`
contributors, not just successful parsing) and
`TestPublicConfigDisabledSignalMatchesExistingConventions`.

Full `v0.7` regression re-run (existing tests, not new ones): all
`scenario_test.go` tests (task 032), all delegation tests (task 031),
all approval-policy tests (task 030), the 1,000-`SessionID` cardinality
test and JSON-compatibility tests (task 014), and every `v0.6`
sequence/transition/n-gram/Markov test — all under
`go test ./... -race -count=1`.

## Example Plan

`examples/ai-agent-security` rewritten to demonstrate the full combined
scenario — delegation, sequence, and approval evidence together —
using only `config.PolicyConfig`/`config.AnomalyConfig`/root
`trustvian`/`alert`, verified with real `go run .` output captured into
its README, and via `make examples` alongside all seven other
examples.

## Security Assertions (re-verified, not re-designed)

- **Policy owns the approval requirement; the event cannot override
  it.** Unchanged from task 030 — re-verified by the full task 030
  regression re-run.
- **`ApprovalStatus`/`DelegatedFrom` remain unverified, self-reported
  evidence.** Unchanged from tasks 030/031 — this task's new
  `AnomalyConfig` exposes `DelegationWeight` as a *scoring* knob, never
  a provenance-verification mechanism; enabling it does not make
  Trustvian authenticate `Context.DelegatedFrom`.
- **`SessionID` still never enters `Fingerprint`/`baseline.Key`
  identity.** Re-verified by re-running the 1,000-distinct-`SessionID`
  test unchanged.
- **No new state-size exposure.** `AnomalyConfig` exposes zero
  bound/limit fields; `internal/baseline`'s existing bounds stay
  hardcoded (ADR 0017).

## Performance

No new production hot-path cost: `CompileAnomaly` runs once at `Engine`
construction (`BenchmarkCompileAnomaly`: ~56 ns/op, 1 alloc — startup
work, not per-event), and `BenchmarkEngineAnalyzeDelegationAbsent`'s
allocation profile remains byte-for-byte identical to
`BenchmarkEngineAnalyze` (`456 B/op, 17 allocs/op`) — confirmed
unchanged by this task, not merely assumed. Full `go test -bench=.
-benchmem ./...` re-run across every package with zero regression.

## Documentation

- [docs/adr/0017-public-anomaly-configuration-boundary.md](../adr/0017-public-anomaly-configuration-boundary.md)
  (new): the full design.
- [docs/DOMAIN.md](../DOMAIN.md), [docs/SECURITY.md](../SECURITY.md),
  [docs/ARCHITECTURE.md](../ARCHITECTURE.md),
  [docs/PERFORMANCE.md](../PERFORMANCE.md): the public configuration
  boundary documented at each doc's own existing level of detail.
- [README.md](../../README.md): "how do I turn this on" answered for
  every major configurable `v0.6`/`v0.7` capability, through public
  config, not internal types.
- [docs/ROADMAP.md](../ROADMAP.md): task 033's own outcome and the
  final `v0.7` release decision.
- [CHANGELOG.md](../../CHANGELOG.md) / [trustvian-project-spec.md](../../trustvian-project-spec.md):
  this task's actual deliverable, accurately scoped.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new test
  above and every pre-existing `v0.1`–`v0.7` test.
- `TestCompileAnomalyZeroValueMatchesDefaultConfig` passes — the
  mandatory backward-compatibility proof.
- `TestPublicConfigConfiguresDelegationAndNGramSignals` passes, run
  from within the genuinely separate `examples` module — the mandatory
  external-consumer, end-to-end configuration proof.
- `--anomaly-config` is wired through both `analyze` and
  `baseline build`, fails closed on an invalid or missing file, and
  leaves existing `--config`-only usage byte-for-byte unchanged.
- No new package, no new pipeline stage, no new anomaly detector, no
  new dependency, no change to `internal/anomaly`'s own public
  surface, no processor change — verified by reviewing the diff
  against this constraint (`git diff -- go.mod go.sum processor/go.mod
  processor/go.sum` empty).
- `BenchmarkEngineAnalyzeDelegationAbsent` confirms zero added hot-path
  allocation.
- A final release-readiness decision (`v0.7.0 RELEASE READY: YES` or
  `NO`, with an explicit blocker list if `NO`) is recorded — this
  task's own defining deliverable, per its "release gate" name.
