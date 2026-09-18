# 039 — CI & Quality Gate Automation

**Milestone:** v0.9 — Operational Readiness · **Depends on:** `v0.8`
(shipped) — the modules, integration mechanism, and reference deployment
this task automates · **Blocks:** 040 (release artifacts), which must run
on gates that already exist · **First slice of `v0.9`.**

## Objective

Convert the quality gates this project has run by hand on every task since
`v0.1` into deterministic CI enforcement, across every module and boundary
a release depends on.

## Why this is first

Everything later in `v0.9` — release artifacts, container publishing,
runtime lifecycle — is safer when correctness is enforced by a machine
rather than by discipline. A release pipeline built on unenforced gates
automates the publishing of unverified artifacts.

The audit opening this milestone found the existing workflow
(`.github/workflows/go.yml`) is the stock GitHub Go template:

| | Present | Gap |
|---|---|---|
| Root module | `go build`, `go test` | no `gofmt`, no `go vet`, no `-race` |
| Processor module | — | never built or tested in CI at all |
| Examples module | — | never built or tested in CI at all |
| PostgreSQL integration | — | `v0.8`'s entire persistence guarantee is unverified in CI |
| Reference deployment | — | Compose config never validated |
| Triggers | PRs to `main`, pushes to `main` | **development happens on `develop`** — so CI effectively runs only at release time |

That last row is the most consequential. The repository's actual flow is
feature → `develop` → PR → `main`, and `v0.7.0`/`v0.8.0` were both tagged
on a `main` merge commit. CI that only watches `main` gives no feedback
during the work and first reports at the moment of release.

## Scope

```text
.github/workflows/ci.yml        quality gates — the work of this task
.github/workflows/nightly.yml   expensive tiers on a schedule
CONTRIBUTING.md                 contributor-facing gate documentation
```

Gates enforced, matching the repository's documented local commands:

| Gate | Command | Modules |
|---|---|---|
| Format | `gofmt -l .` (non-empty output fails) | root, processor, examples |
| Vet | `go vet ./...` | root, processor, examples |
| Build | `go build ./...` | root, processor, examples |
| Test | `go test ./...` | root, processor, examples |
| Race | `go test -race ./...` | root, processor |
| Isolation | `GOWORK=off` for processor and examples | — |
| Persistence | PostgreSQL service container + `TRUSTVIAN_TEST_POSTGRES_DSN` | root |
| Deployment | `docker compose config` | reference deployment |

## Non-Goals

- **No publishing of anything.** No GitHub Release, no container image, no
  binaries, no SBOM, no signing. Those are 040 and 041, and keeping them
  out is what allows this workflow to run with read-only permissions.
- **No module-version changes.** `processor/go.mod`'s development
  `replace` is deliberately left alone; it is 040's subject. CI must work
  with the repository as it is, not require it to change first.
- **No new lint tooling.** `gofmt` and `go vet` are the repository's
  existing standard. Adding `golangci-lint` here would introduce a new
  failure surface and a new dependency for a task about enforcing existing
  gates.
- **No source modification in CI.** Validation reports formatting
  violations; it never rewrites code.
- **No rewriting of task 035/036 tests.** CI reuses the existing
  `TRUSTVIAN_TEST_POSTGRES_DSN` mechanism exactly.
- **No new CI platform.** GitHub Actions is already in use.
- **No committed generated output** — no coverage files, binaries, or SBOM.

## Architecture constraints

- **CI commands mirror local commands.** A developer running `make check`
  and CI must exercise the same thing; a CI-only test path is a path
  nobody can debug locally.
- **Module isolation is verified, not assumed.** The processor and
  examples modules run with `GOWORK=off`, so a workspace cannot mask a
  broken module boundary. This is release-critical: `processor/go.mod`
  resolves the core module through a `replace`, and only a workspace-free
  build proves that resolution is real.
- **Existing `Makefile` targets remain the local entry point.** CI does
  not replace them.

## Security requirements

- **Least privilege.** `permissions: contents: read` at workflow level.
  No `contents: write`, `packages: write`, or `id-token: write` — this
  workflow publishes nothing, so it needs no write scope anywhere.
- **No secrets.** Every gate runs on public code and ephemeral local
  credentials. Fork pull requests therefore cannot exfiltrate anything,
  because there is nothing to exfiltrate.
- **No `pull_request_target`.** The plain `pull_request` event runs fork
  code without repository write scope or secret access. Using
  `pull_request_target` would run trusted-context code against untrusted
  input for no benefit here.
- **Ephemeral database credentials only.** The PostgreSQL service
  container uses throwaway values scoped to one job, never a production
  DSN.
- **Pinned actions.** Actions referenced by major version tag, matching
  the repository's existing usage.

## Test tier classification

Not every test belongs on every commit. Tests are classified by cost and
by what a failure would mean:

| Tier | What runs | Rationale |
|---|---|---|
| **PR** | format, vet, build, test, race, all three modules, PostgreSQL integration (`-short`), Compose config | Correctness gates. A PR that breaks any of these is broken. |
| **Main** | identical to PR | Catches anything that merged through a stale branch. |
| **Nightly** | full PostgreSQL stress tier (no `-short`), plus the Compose smoke test | Expensive and time-dependent; valuable as a trend, wasteful per-commit. |
| **Release** | inherited from nightly by 045's gate | Release verification is 045's scope, not this task's. |

`-short` is the existing, documented mechanism for this split (see
`internal/store/postgres/stress_test.go`); this task adds no second one.

## Implementation requirements

1. PostgreSQL runs as a GitHub Actions **service container** with a health
   check, so jobs wait on readiness rather than sleeping.
2. Every job sets an explicit `timeout-minutes`.
3. Go version comes from `go.mod` (`go-version-file`), so the toolchain
   cannot drift from the module's own declaration. No invented support
   matrix: the repository states one supported version.
4. Dependency caching via `actions/setup-go`'s built-in cache. No custom
   cache infrastructure.
5. Triggers cover `main` **and** `develop`, for both `push` and
   `pull_request`, so day-to-day work is actually gated.

## Tests

CI is itself the test. Verification for this task is:

- every CI command executed locally and passing, including the
  `GOWORK=off` module-isolation runs and the PostgreSQL integration tier;
- the workflow YAML structurally validated rather than eyeballed;
- confirmation that a formatting violation actually fails the format gate
  (a gate that cannot fail is not a gate).

## Documentation

- `CONTRIBUTING.md` (new) — how to run the gates locally, what CI
  enforces, and the tier split. This is the repository's first
  contributor-facing document; it stays short.
- `docs/ROADMAP.md` — `v0.9` slice sequence.
- `CHANGELOG.md` — a maintainer-visible entry under a new `Unreleased`
  section.
- **`README.md` carries no milestone status**, and this task adds none. Its
  one change is the CI badge URL, which must follow the workflow file from
  `go.yml` to `ci.yml` — a link correction, not project-status prose.

## Acceptance Criteria

1. CI runs on `pull_request` and `push` for both `main` and `develop`.
2. A `gofmt` violation fails CI, proven rather than assumed.
3. `go vet` failures fail CI.
4. Root module tests and `-race` tests run.
5. Processor module builds, vets, tests, and race-tests with `GOWORK=off`.
6. Examples module is validated independently of the workspace.
7. PostgreSQL integration runs against a service container using the
   existing `TRUSTVIAN_TEST_POSTGRES_DSN` mechanism, with no test modified.
8. Reference deployment Compose configuration is validated.
9. Expensive stress tests are classified and scheduled, not run per-commit.
10. Workflow permissions are read-only; no secrets are required or
    referenced; fork PRs receive none.
11. Nothing is published — no release, image, or artifact.
12. Local `Makefile` workflow remains usable and unchanged in behavior.
13. Every CI command verified locally.
14. Documentation synchronized; `README.md` gains no milestone status (its
    only edit is the CI badge URL).
