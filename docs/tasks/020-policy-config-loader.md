# 020 — Config File Loader & Schema v1 Parsing

**Milestone:** v0.5 · **Depends on:** [019](019-policy-config-model.md)
(the public `PolicyConfig`/`PolicyRule`/`PolicyCondition` model and
`CompilePolicy` this task decodes into and validates through) ·
**Blocks:** CLI `--config` integration, `processor/` integration, and
Alert configuration (all separate, later v0.5 tasks — see Non-Goals)

## Objective

Let a caller load a versioned Trustvian configuration file into the
existing public `config.PolicyConfig` model safely and
deterministically — `trustvian.yaml → Load → Validate → CompilePolicy`
— without inventing a second, file-specific policy model, and without
wiring this into the CLI or the OTel Collector processor yet (each is
its own later vertical slice).

## Why

Task 019 gave a caller a way to construct a `PolicyConfig` in Go. That
alone doesn't yet make Trustvian "configurable as a standalone OSS
product" — a real deployment wants to author a YAML file, not write
Go. This task is the next narrowest slice toward that: a loader that
turns file bytes into the *same* `PolicyConfig` type task 019 already
defined, validated, and ready for `CompilePolicy`.

## Scope

```text
bytes/file
  ↓ Load / LoadFile
strict YAML decode (unknown fields rejected)
  ↓
config.PolicyConfig  (task 019's existing type — not a new one)
  ↓ (Validate(), called internally)
validated config.PolicyConfig
  ↓ CompilePolicy (task 019, unchanged)
policy.Policy
  ↓ trustvian.WithPolicy (unchanged)
Engine
```

- `yaml:"..."` struct tags added to the existing `PolicyConfig`/
  `PolicyRule`/`PolicyCondition` types (task 019) — a purely additive
  change; no field renamed, removed, or retyped, so existing Go-literal
  construction of these types is unaffected.
- `Load(data []byte) (PolicyConfig, error)` — decodes `data` as
  schema-v1 YAML with `Decoder.KnownFields(true)` (strict: an
  unrecognized field anywhere in the document fails, rather than being
  silently ignored), then calls `Validate()` before returning.
- `LoadFile(path string) (PolicyConfig, error)` — a thin wrapper: opens
  `path`, reads at most `maxConfigFileSize` (1 MiB) bytes, and calls
  `Load`. `path` is caller-controlled; `LoadFile` does not scan
  directories, auto-discover a file, or apply its own traversal
  policy.
- One new dependency: `go.yaml.in/yaml/v3` (see Dependency Review in
  the accompanying report for the selection rationale — its checksums
  were already present in `go.sum` before this task, from an earlier,
  unrelated dependency-graph resolution).

## Non-Goals

- **No CLI integration.** `cmd/trustvian` is not touched; no
  `--config` flag exists after this task.
- **No OTel Collector processor integration.** `processor/` continues
  running the default `Policy`.
- **No Alert configuration.** No `AlertConfig`, no `alerts:` YAML
  section — `config.PolicyConfig` is the only thing this loader
  produces.
- **No JSON support.** YAML only, per this milestone's own "keep v1
  small" principle — see [ROADMAP.md § v0.5](../ROADMAP.md#v05--policy--configuration).
- **No environment-variable interpolation** (`${VAR}` or similar) —
  secret/environment resolution is a separate, deliberately deferred
  design decision, not folded into this loader.
- **No live/hot reload.** `Load`/`LoadFile` are load-time-only; no file
  watching, no atomic swap, no rollback semantics.
- **No new file-specific policy model.** The loader decodes directly
  into task 019's existing `PolicyConfig`/`PolicyRule`/
  `PolicyCondition` — there is no parallel `FileConfig` type.
- **No numeric/NaN/Inf validation** — `PolicyCondition` has no numeric
  matcher field today (unchanged from task 019); nothing for this
  loader to validate on that axis.

## Technical Requirements

- Strict decoding is unconditional — there is no option to disable
  unknown-field rejection. A config-time typo (e.g.
  `default_decison: block`) must fail loudly, not silently fall back
  to any other decision.
- Duplicate YAML mapping keys (e.g. two `decision:` entries in one
  rule) are rejected — verified as `go.yaml.in/yaml/v3`'s own default
  `Decoder` behavior empirically, not assumed of the library (see
  `TestLoadRejectsDuplicateYAMLKeys`).
- Empty input produces a distinct, actionable `ErrEmptyInput` — not a
  bare `io.EOF` surfaced from the underlying decoder.
- `LoadFile` bounds its read to `maxConfigFileSize` (1 MiB, documented
  rationale in code) via `io.LimitReader(f, maxConfigFileSize+1)` and
  an explicit length check — detects an over-limit file rather than
  silently truncating and parsing partial content.
- YAML sequence order is preserved into `PolicyConfig.Rules` exactly —
  proven by test, not assumed of the decoder.
- `Load` calls `Validate()` internally — a caller never receives a
  syntactically-valid-but-semantically-invalid `PolicyConfig` from
  `Load`.
- No package-level mutable state, no caching, no global parser
  instance (`.claude/rules/go.md`).

## Tests

`config/load_test.go`: minimal valid YAML; full YAML (multiple rules,
`when`/`unless`, `attributes`); rule order preserved; a loaded config
compiles successfully via `CompilePolicy`; empty input; malformed
YAML; missing/unsupported schema version; unknown top-level field;
unknown nested field (inside `when:`); invalid decision value; invalid
`actor_type`/`operation_category`/`min_risk_level`; duplicate rule
names; duplicate YAML keys; over-limit rule count reached through a
real YAML document (not just `Validate()` directly); `LoadFile`
rejects an oversized file; `LoadFile` surfaces a clear I/O error for a
missing file; a small "arbitrary bytes must not panic" smoke test.
`config/fuzz_test.go`: `FuzzLoad`, the same invariant as a real fuzz
target, seeded from the valid/invalid corpus already used elsewhere in
the test suite.

The load-bearing integration test:
`TestEndToEndYAMLFileChangesEngineDecision` — a real file on disk,
`LoadFile` → `CompilePolicy` → a real `Engine` → a real `Analyze` call
against a cold-start, `RiskCritical` event → confirms the *loaded*
rule, not the built-in default, produced `BLOCK`.

## Benchmarks

Not required by this task's own acceptance criteria (parsing is
startup-path work, per this milestone's existing precedent from task
019 — see `docs/tasks/019-policy-config-model.md` § Benchmarks). Not
added here to avoid measuring a path with no current performance
concern.

## Documentation

- [policy-guide.md](../policy-guide.md): a minimal, real YAML example
  alongside task 019's existing Go-literal example.
- [SECURITY.md](../SECURITY.md): document strict unknown-field
  handling and the file-size bound as this task's addition to the
  existing Configuration-input validation entry.
- [ROADMAP.md](../ROADMAP.md): mark this task done under `v0.5`; keep
  CLI/processor/Alert-config integration explicitly not-yet-started.
- [getting-started.md](../getting-started.md): a short pointer to
  `config.LoadFile`, without describing CLI `--config` (not
  implemented).

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new `config`
  test.
- The full `Config File → Load → Validate → CompilePolicy → Engine →
  Decision` chain is proven by a real end-to-end test — not unit
  tests on the decoder alone.
- Strict unknown-field rejection and duplicate-key rejection both
  proven by test, not assumed of the YAML library.
- Exactly one new dependency (`go.yaml.in/yaml/v3`), justified in the
  accompanying report; no dependency added to `processor/go.mod` or
  `examples/go.mod`.
- `go list -deps` confirms the core detection engine (`event` through
  `internal/policy`, `Engine`) does not import `config` or
  `go.yaml.in/yaml/v3`.
- `gofmt -l .`, `go vet ./...` clean.
