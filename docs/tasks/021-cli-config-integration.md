# 021 — CLI Configuration Integration

**Milestone:** v0.5 · **Depends on:** [019](019-policy-config-model.md)
and [020](020-policy-config-loader.md) (the public `config` package
this task consumes, unmodified) · **Blocks:** OTel Collector processor
integration and Alert configuration (both separate, later v0.5 tasks —
see Non-Goals)

## Objective

Let `trustvian analyze`/`trustvian baseline build` consume the
canonical `config` package directly — `--config <path> →
config.LoadFile → config.CompilePolicy → trustvian.WithPolicy → Engine`
— so a user can get real `ALLOW`/`BLOCK`/`REQUIRE_APPROVAL`
differentiation from a YAML file, without embedding Go code, and
without the CLI reimplementing any parsing or validation logic tasks
019/020 already own.

## Why

The CLI already constructs a differentiated (non-default) `Policy` —
`defaultPolicy()` in `cmd/trustvian/policy.go`, a small built-in
starter policy predating this task — but that policy is fixed in Go
source; a user cannot change it without recompiling the binary. This
task adds the one thing the CLI was missing to make it a genuine
consumer of `v0.5`'s new configuration boundary: an explicit,
optional way to supply a different `Policy` from a file at invocation
time.

## Scope

```text
--config <path>
      ↓
config.LoadFile(path)
      ↓
config.CompilePolicy(cfg)
      ↓
trustvian.WithPolicy(p)
      ↓
trustvian.NewEngine(...)
      ↓
existing analyze/baseline build flow, unchanged
```

- `--config <path>`, an optional flag (via the standard library's
  `flag` package — no flag framework introduced), added to **both**
  `trustvian analyze` and `trustvian baseline build`. Both subcommands
  already shared one engine-construction helper
  (`newEngine`, `cmd/trustvian/policy.go`) before this task; extending
  that one helper is what makes both subcommands support the flag
  without duplicating engine-construction logic across them.
- `newEngine` gains a `configPath string` parameter and an `error`
  return: with `configPath == ""`, it is byte-for-byte the same call it
  always was (`trustvian.NewEngine(trustvian.WithPolicy(defaultPolicy()))`) —
  this is the hard compatibility requirement this task must not
  regress. With `configPath` set, it calls `config.LoadFile` then
  `config.CompilePolicy`, returning either error unwrapped-or-lightly-wrapped
  (see Technical Requirements) rather than falling back to
  `defaultPolicy()`.
- Usage text (`cmd/trustvian/main.go`) updated to document the flag.
- Existing `defaultPolicy()` (which predates this task and already
  imports `internal/policy`/`internal/trust` for its own, unrelated
  reason — it is in-module code building a Go literal, the same way
  any other in-module caller could) is untouched. This task's own new
  code path never imports `internal/policy`.

## Non-Goals

- **No CLI-specific policy/config model.** No `CLIConfig`, no
  `CLIPolicy`. The CLI decodes into the exact same
  `config.PolicyConfig` any other caller would.
- **No new config semantics.** No new condition fields, decisions, or
  schema version introduced to satisfy a CLI need — if the CLI needed
  something `config`/`policy.Condition` doesn't support, that would be
  a separate config-model task, not something to add here.
- **No environment variable configuration** (`TRUSTVIAN_CONFIG` or
  similar) — the explicit `--config` flag only.
- **No config auto-discovery** (`./trustvian.yaml`,
  `$HOME/.trustvian.yaml`, `/etc/trustvian/...`) — an omitted
  `--config` always means "use `defaultPolicy()`," never "search for a
  file."
- **No live reload.** Configuration loads once per CLI invocation, as
  it always would for a short-lived process — no watching, no atomic
  swap.
- **No OTel Collector processor integration.** `processor/` is not
  touched.
- **No Alert configuration.** No `--alert-config`, no `AlertConfig`.
- **No config mutation/normalization/migration commands.**

## Technical Requirements

- `newEngine("")` must be observably identical to the pre-task
  `newEngine()` — proven by the full, unmodified pre-existing test
  suite (`TestRunAnalyzeNormalEventIsAllowed`,
  `TestRunAnalyzeAnomalousEventIsBlocked`,
  `TestRunBaselineBuildSummary`, etc.) still passing unchanged.
- An explicit, invalid, or missing `--config` must fail the whole
  command — non-zero exit, no report printed to stdout, no fallback to
  `defaultPolicy()`. This is checked structurally, not just by
  intent: `newEngine`'s config-loading branch returns on the first
  error, before `trustvian.NewEngine` is ever called with anything
  other caller-visible config.
- `config.LoadFile`'s own errors (which already include the config
  path — see task 020) are returned as-is; only `CompilePolicy`'s
  errors (which have no path context, since `Validate` operates on an
  in-memory value) are wrapped with the path for CLI-level context.
  Neither path is destroyed or replaced with a generic "invalid
  config" message.
- Errors go to stderr via the CLI's existing `run()`-level
  `"trustvian: %v\n"` formatting — unchanged, no new output channel.
  Analysis output continues to go to stdout exclusively, and only
  after a successful `Engine` construction.
- `flag.NewFlagSet(..., flag.ContinueOnError)` with `SetOutput(io.Discard)`
  is used per subcommand so flag-parsing errors are folded into each
  subcommand's own existing `"usage: ..."` error message, not a
  separately-formatted `flag` package error.
- Flag ordering: for `baseline build`, `--config` is parsed *after*
  the fixed `"build"` token is consumed
  (`trustvian baseline build --config <path> <events.json>`), not
  before it — Go's `flag.Parse` stops at the first non-flag argument,
  so `"build"` must be consumed first or a `--config` placed after it
  would never be recognized as a flag.

## Tests

`cmd/trustvian/main_test.go`, added:

- `TestRunAnalyzeConfigOverridesDefaultDecision` — the central
  acceptance test: the exact same event `TestRunAnalyzeNormalEventIsAllowed`
  proves resolves to `ALLOW` under the default policy resolves to
  `BLOCK` once a `--config` file supplying a catch-all block rule is
  given — proving `--config` changes real behavior, not merely that it
  parses.
- `TestRunAnalyzeInvalidConfigFailsClosed` — a config with an
  unrecognized field (`default_decison`, a typo) fails the whole
  command: non-zero exit, empty stdout, and an error naming the
  unrecognized field — proving strict decoding (task 020) survives the
  CLI boundary rather than being silently bypassed by how the CLI
  calls the loader.
- `TestRunAnalyzeMissingConfigFailsClosed` — a nonexistent `--config`
  path fails the same way: non-zero exit, empty stdout, no
  auto-discovery of any other file.
- `TestRunBaselineBuildAcceptsConfigFlag` — `baseline build` accepts
  `--config` too and it changes learning behavior (every event is
  ineligible for learning under the configured block-all policy,
  unlike the corpus's default-policy behavior in the pre-existing
  `TestRunBaselineBuildSummary`) — proving the flag was wired into both
  subcommands' shared engine construction, not just `analyze`'s.
- Every pre-existing test in the file passes unmodified — the explicit
  default-behavior-preservation regression check.

New fixtures: `cmd/trustvian/testdata/policy-block-all.yaml` (a valid,
minimal, catch-all-block policy, used to prove `--config` actually
changes behavior) and `testdata/policy-invalid.yaml` (the unknown-field
regression fixture).

## Benchmarks

Not applicable — no hot-path or core engine change. Config loading
remains the same startup-path-only operation task 020 already
benchmarked; the CLI does not change how or how often it runs.

## Documentation

- [README.md](../../README.md): the `Go SDK`/CLI sections' existing
  "no CLI `--config` flag exists yet" language is now stale; corrected.
- [docs/cli-guide.md](../cli-guide.md): document `--config`, default
  behavior without it, invalid/missing-file behavior, and a link to
  the Policy Guide for the file format itself (not duplicated here).
- [docs/getting-started.md](../getting-started.md): the "no CLI
  `--config` flag yet" note is now stale; corrected.
- [ROADMAP.md](../ROADMAP.md): mark this task done under `v0.5`; keep
  Collector integration and Alert configuration explicitly
  not-yet-started.
- [CHANGELOG.md](../../CHANGELOG.md): this repository's own convention
  is one heading per tagged version, with no `Unreleased` section ever
  used (verified: `v0.4.0`/`v0.3.0`/`v0.2.0`/`v0.1.0` are the only
  headings present). The existing `v0.5.0` tag itself predates this
  task and covers only 019/020 — its own changelog heading was
  missing entirely (a pre-existing gap, fixed here by adding it,
  scoped to 019/020 to match what that tag actually contains). Task
  021's own entry is deferred to whatever future tag captures it,
  consistent with how every other entry in this file was added at
  tag-time, not per-task.

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new
  `cmd/trustvian` test, and every pre-existing `cmd/trustvian` test
  unmodified and still passing.
- A real CLI invocation with `--config` produces a different
  `Decision` than the same invocation without it, for the same input
  event — demonstrated by both an automated test and a manual smoke
  test with a real built binary.
- A real CLI invocation with an invalid or missing `--config` exits
  non-zero, prints no analysis report, and does not fall back to
  `defaultPolicy()` — demonstrated the same way.
- `gofmt -l .`, `go vet ./...` clean.
- No new dependency (`git diff go.mod go.sum` empty).
- `cmd/trustvian` does not import `internal/policy` in any code path
  introduced by this task (the pre-existing `defaultPolicy()` import
  is unaffected and out of scope).
