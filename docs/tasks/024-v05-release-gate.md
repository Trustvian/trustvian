# 024 — v0.5.0 Release Gate

**Milestone:** v0.5 · **Depends on:**
[019](019-policy-config-model.md)–[023](023-declarative-alert-configuration.md)
· **Blocks:** `v0.6` (start it, or design it in more depth, only once
`v0.5.0` is a real, tagged release) · **Mirrors:** [013](013-oss-v01.md),
the `v0.1` release gate — same shape, applied to `v0.5`.

## Objective

Not new functionality — a checklist and gate tying tasks 019–023
together into an actual tagged, publicly resolvable `v0.5.0` release.
This task's deliverable is a verified checklist and a release, not
code.

## Why

`v0.1`'s own release gate ([013](013-oss-v01.md)) established why this
matters: a tagged release is the first point external users can depend
on a specific, stable snapshot rather than a moving `develop` branch.
`v0.5` carries one complication `v0.1` didn't have to consider: a
`v0.5.0` git tag already exists **locally**, created before this gate
ran, pointing at an earlier commit (`b9763e3`) that only contains tasks
019–020. That tag was never pushed to `origin` — closing this gate
means resolving that, not just running the usual checks.

**Verified, not assumed:** `main` and `develop` are currently
byte-identical (`git diff main develop` is empty) — every prior
release tag in this repository's history (`v0.1.0`–`v0.4.0`) points at
a "Merge pull request #NN from Trustvian/develop" commit on `main`,
confirming this repository's actual convention is *tag on `main`, at
the commit where `develop`'s work lands there*, not on `develop`
directly. `main`'s current tip already contains all of 019–023 —
it is a real, present-day candidate for the `v0.5.0` release commit,
not a hypothetical future one this gate has to wait for.

## Scope

- Verify every task 019–023 individually meets its own acceptance
  criteria (this task does not re-derive them — it checks the box
  each already defined).
- Verify the full existing baseline still holds after all of `v0.5`'s
  changes land together, not just individually: `go build ./...`,
  `go vet ./...`, `go test -race ./...`, `gofmt -l .` all clean at the
  repository root; `go mod tidy` at the root produces no diff (no
  accidental new dependency snuck in across the whole milestone).
- Verify `processor/` independently, first with a local `go.work`
  (since, before the tag existed, its committed `go.mod` could not yet
  resolve `config`), then — once the tag is pushed and
  `processor/go.mod` is bumped — with a clean, workspace-free
  `GOWORK=off go build ./... && GOWORK=off go test ./... -race`
  against the real, published release. Confirm each stage empirically
  rather than assuming it.
- Re-run a documentation consistency check across every `.md` file
  touched or referenced by 019–023 (stale "first two tasks"/"not yet
  done" phrasing, dangling anchors, task-file cross-references) — the
  same kind of cross-task inconsistency check task 013 ran for `v0.1`.
- Resolve the pre-existing local-only `v0.5.0` tag: it must be moved to
  (or replaced by, if moving is not possible/appropriate) the actual
  release commit before being pushed — a stale tag pointing at a
  commit that doesn't contain 021–023 would misrepresent the release.
- Decide and document the version-compatibility promise `v0.5.0` adds
  on top of `v0.1`'s existing one: the new public symbols in `config`
  (`PolicyConfig`, `AlertConfig`, and everything each compiles/loads)
  join the [CHANGELOG.md § Public API compatibility
  promise](../../CHANGELOG.md#public-api-compatibility-promise) from
  this release onward.
- Push the resolved tag; update `processor/go.mod` to depend on it in
  a small, dedicated follow-up commit; verify that follow-up with
  `GOWORK=off go mod download && go list -m github.com/Trustvian/trustvian`
  reporting `v0.5.0` and a clean `processor/` build/test with no
  workspace involved.

## Non-Goals

- No feature work of any kind — if this gate discovers a gap in
  019–023, it blocks on the relevant task being finished properly, or
  gets explicitly deferred to a documented follow-up, never "quickly
  patched" as part of the gate itself.
- No `v0.6` work of any kind (sequence analysis, AI-agent extensions,
  or anything else on this roadmap's later milestones).
- No CI/CD pipeline build-out for future releases — that is separately
  scoped, unstarted `v0.9` work.

## Technical Requirements

N/A — this is a verification and release task, not an implementation
one.

## Tests

- The full existing root test suite, run fresh, not from cache:
  `go test ./... -race -count=1`.
- `processor/`'s test suite, run the same way, using the local
  `go.work` (see Scope above for why `GOWORK=off` cannot succeed yet).

## Benchmarks

- The full benchmark suite (root and `processor/`), run fresh, results
  reviewed against [PERFORMANCE.md](../PERFORMANCE.md)'s existing
  baseline for any unexpected regression — none of 019–023 touch a
  runtime hot path (all new compilation/validation work is
  startup-path only), so none is expected.

## Documentation

- [ROADMAP.md](../ROADMAP.md): "Current status" reflects `v0.5` as
  implementation-complete/release-ready, distinctly from "shipped,"
  until this gate's tag actually lands; a "Release readiness — v0.5.0"
  cross-reference to this task's own checklist below.
- [CHANGELOG.md](../../CHANGELOG.md): the prepared `v0.5.0` entry's
  "prepared, not yet published" notice is removed once the tag is
  actually pushed — that edit is this gate's own final documentation
  step, not a preemptive one.
- [README.md](../../README.md): "Status" section updated to say
  `v0.5.0` shipped once (and only once) the tag exists.

## Release Checklist

```text
v0.5.0 release gate

[x] scope complete (019, 020, 021, 022 code, 023)
[x] Alert config complete (023)
[x] root tests                          (go test ./...)
[x] root race                           (go test -race ./...)
[x] root vet                            (go vet ./...)
[x] root formatting                     (gofmt -l .)
[x] benchmarks reviewed                 (no hot-path regression)
[x] processor tests (local go.work)     (go test ./... -race, in processor/)
[x] processor race (local go.work)
[x] processor vet (local go.work)
[x] docs consistent                     (see this task's own Scope)
[x] CHANGELOG final                     (published; "not yet published" notice removed once tag was live)
[x] public API review                   (see task's own writeup / this task's report)
[x] config compatibility                (schema v1 untouched; verified by full existing test suite)
[x] security review                     (docs/SECURITY.md updated for Alert config)
[x] human creates/pushes v0.5.0 tag        — the stale local-only tag
    was moved to main's tip (containing 019-023) and pushed;
    `git ls-remote --tags origin` and a real `go get
    github.com/Trustvian/trustvian@v0.5.0` both confirm it resolves
[x] processor go.mod prepared for v0.5.0   — done in a separate,
    post-tag follow-up commit (go.sum entries are content hashes of
    the published module; nothing could compute them before the tag
    existed — verified directly, not assumed, before this step ran)
[x] GOWORK=off processor verification after tag — `GOWORK=off go
    build ./... && GOWORK=off go test ./... -race` both pass against
    the real, published v0.5.0, no workspace involved
[ ] GitHub release notes                   — drafted, not yet published
```

## Acceptance Criteria

- Every dependency task (019–023) independently verified complete
  against its own acceptance criteria.
- `go build`/`go vet`/`go test -race`/`gofmt -l`/`go mod tidy` all
  clean on the exact commit intended for the tag, at the repository
  root.
- `processor/` builds and tests clean under a local `go.work` — and,
  once the tag exists, cleanly with `GOWORK=off` and no workspace at
  all.
- The full-repository documentation consistency check (per Scope)
  passes with zero findings.
- A version tag exists on `origin`, `processor/go.mod` depends on it,
  and a `GOWORK=off` build/test of `processor/` succeeds against that
  real, published dependency — **met**: `v0.5.0` is pushed to `origin`,
  `processor/go.mod` requires it, and `GOWORK=off go build ./... &&
  GOWORK=off go test ./... -race` both pass with no workspace involved.
