# 040 — Release Artifacts & Module Consistency

**Milestone:** v0.9 — Operational Readiness · **Depends on:**
[039](039-ci-quality-gate-automation.md) (the gates a release must pass) ·
**Blocks:** 041 (container supply chain), which packages the binaries this
task produces · **Second slice of `v0.9`.**

## Objective

Make Trustvian releases reproducible and consumable as versioned
Go/binary artifacts, and settle how this repository's independently built
modules resolve Trustvian versions without depending on a
developer-local workspace.

## What the audit found

**Releases `v0.1.0`–`v0.8.0` were produced entirely by hand.** There is no
release workflow, no release script, no `Makefile` release target, and no
`.goreleaser.yml`. Every tag was created manually on a `main` merge commit
and the GitHub Release written by hand. No binaries have ever been
attached to a release — Trustvian has been consumable only via
`go install`.

**The nested modules cannot be published, by module path.** This is the
finding that reframes the whole task:

| Module | Declared path | Resolvable by `go get`? |
|---|---|---|
| root | `github.com/trustvian/trustvian` | **Yes** — tagged `v0.1.0`–`v0.8.0` |
| processor | `trustvian-processor` | **No** |
| examples | `trustvian-examples` | **No** |

`go get trustvian-processor` fails with *"malformed module path: missing
dot in first path element"*. A bare name is not a resolvable module path,
so neither nested module is publishable in its current form, and no
`processor/vX.Y.Z` tag has ever existed.

That means the `replace github.com/trustvian/trustvian => ../` in
`processor/go.mod` is **not** a latent release blocker awaiting removal. It
is the correct and consistent arrangement for a module that is built from
this repository rather than fetched from a proxy — which is exactly how
`processor/README.md` documents running it (`cd processor && go run
./cmd/trustvian-collector`, explicitly "no `ocb` install required").

## Module publication model

This classification is the durable output of this task. It is recorded
here and in the release guide so no future maintainer has to re-derive it
mid-release.

**`github.com/trustvian/trustvian` (root) — PUBLIC VERSIONED MODULE.**
The engine, public API (`event`, `config`, `alert`, root package), and the
`trustvian` CLI. Tagged `vX.Y.Z` on the `main` merge commit. It contains no
`replace` directive and must never contain one: a `replace` in a published
module's own `go.mod` is ignored by consumers, so it silently means the
published module does not build the way the repository does.

**`trustvian-processor` — REPOSITORY-INTERNAL MODULE.** A separate module
so the heavy OpenTelemetry Collector dependency tree stays out of the root
module's graph — that separation is its purpose and remains correct. It is
*not* published: its path is not resolvable, it has never been tagged, and
its documented usage is building from a clone. It keeps `replace => ../`,
which is what makes that usage work.

**`trustvian-examples` — REPOSITORY-INTERNAL MODULE.** Deliberately so.
Its value is being a module that *cannot* import `internal/*`, proving the
public API is sufficient for an outside consumer. That proof requires it to
be a separate module, not a published one.

### Why not promote the processor now

Making the processor consumable means renaming its path to
`github.com/trustvian/trustvian/processor`, dropping the `replace`, and
adopting nested `processor/vX.Y.Z` tags — with the ordering constraint that
the root module must be tagged before the processor can require a real
version of it.

That is real work with real semantics, and nothing currently needs it: no
external consumer exists, no documentation offers one a path, and the
component is distributed as a runnable binary built from this repository.
Promoting it speculatively is precisely the kind of ahead-of-need
abstraction [ADR 0018](../../../adr/0018-production-store-boundary-and-postgresql-direction.md)
and CLAUDE.md rule out — the same reasoning that kept `Policy` and
`anomaly.Config` internal until a consumer needed them.

**The trigger to revisit** is a concrete external consumer wanting to build
a Collector containing this processor via `ocb`/`otelcol-builder`. At that
point the path rename and nested tagging become justified, and the release
guide records what the transition requires.

## Scope

```text
internal/buildinfo/          version/revision reporting from Go build info
cmd/trustvian/version.go     `trustvian version`
scripts/release-build.sh     cross-compile matrix, archives, checksums
scripts/check-modules.sh     module consistency invariants
.github/workflows/release.yml  tag-triggered release (created, not run)
.github/workflows/ci.yml     + module consistency + release dry-run compile
docs/release-guide.md        maintainer-facing release process
Makefile                     release-dry-run, check-modules targets
processor/go.mod             truthful root-version floor
```

## Non-Goals

Each of these belongs to a later slice and is deliberately absent:

- **No SBOM** (041). **No vulnerability scanning** (041). **No signing**,
  Cosign, Sigstore, GPG, or `id-token` permissions (041).
- **No container image publishing**, no `docker buildx push`, no GHCR
  (041).
- **No health/readiness, self-observability, or graceful shutdown**
  (042/043).
- **No release is published, no tag created, no artifact uploaded** during
  this task. The workflow is written and structurally validated only.
- **No `v0.9.0` tag** and no README claim that `v0.9` shipped.
- **No release framework dependency.** GoReleaser was evaluated and
  rejected: producing five binaries, three archives, and a checksum file is
  ~60 lines of shell, against a large dependency in the release path that
  would need its own supply-chain review — the opposite of what `v0.9` is
  for. If the matrix ever grows (packages, Homebrew taps, multi-registry
  images), that trade-off is worth revisiting.
- **No processor module-path change**, for the reasons above.

## Release artifact matrix

Only the `trustvian` CLI is packaged. It is the repository's one
user-facing executable entry point; `processor/cmd/*` are
repository-internal binaries belonging to a non-published module, and
libraries are distributed as Go modules, not as fabricated binaries.

Targets are advertised only if they compile, verified by building each:

| OS | Arch | Archive |
|---|---|---|
| linux | amd64 | `.tar.gz` |
| linux | arm64 | `.tar.gz` |
| darwin | amd64 | `.tar.gz` |
| darwin | arm64 | `.tar.gz` |
| windows | amd64 | `.zip` |

`CGO_ENABLED=0`: the module's three dependencies (pgx, OTel, yaml) are
pure Go, so nothing requires cgo. Static binaries cross-compile without a
toolchain per target and run on any libc, which is what makes the matrix
buildable from one runner at all.

Determinism: `-trimpath` (no absolute build paths) and no `-ldflags`
injection of timestamps or usernames. Version and revision come from Go's
own build info, which derives them from the VCS state rather than from
whoever ran the build. **Byte-for-byte reproducibility is not claimed** —
it is not tested, and claiming it without evidence would be the kind of
unverified assertion this project avoids.

Archive contents are the binary plus `LICENSE` and `README.md`, nothing
else. Names are deterministic:
`trustvian_<version>_<os>_<arch>.<ext>`.

## Version reporting

`trustvian version` reports the module version, VCS revision, build time,
and dirty flag, read from `runtime/debug.ReadBuildInfo()`.

No `-ldflags` stamping and no package-level version variable: Go already
records `vcs.revision`, `vcs.time`, and `vcs.modified` for any build from a
checkout, and the module version for anything installed via
`go install ...@version`. Using it avoids both a build-flag contract and
the package-level mutable state `.claude/rules/go.md` forbids.

## Module consistency check

`scripts/check-modules.sh`, run in CI, enforces the invariants the model
above depends on — so drift fails a pull request rather than surfacing
during a release:

1. The **root module declares no `replace`**. A published module with a
   replace does not build for consumers the way it builds here.
2. The root module path is exactly `github.com/trustvian/trustvian`.
3. Every nested module that `replace`s the root **must point at `../`** —
   a replace to anywhere else (an absolute developer path, a fork) would
   break every other machine.
4. Every nested module's `require` of the root **must name a version that
   really exists**, so the declared floor is truthful. `processor`'s pinned
   `v0.5.0` predates the `v0.8` APIs it now uses; this task corrects it to
   `v0.8.0`.

   "Exists" is established from either of two independent sources: a local
   git tag (free and offline) or the module proxy (authoritative — it is
   what a consumer resolves against). The first implementation checked only
   for a local tag, which conflated *the version existing* with *this
   checkout having fetched tags*; see § Corrective pass below.
5. Nested modules must not be silently promoted: a nested module whose path
   *is* resolvable (`github.com/...`) must not carry a `replace` to `../`,
   because that combination is exactly the broken-published-module case.

Rule 5 is what makes this check useful *later*: if someone promotes the
processor without doing the rest of the transition, CI says so.

## Release automation

`.github/workflows/release.yml`, triggered by a pushed tag matching `v*`:

1. **Validate** the tag against SemVer (`vMAJOR.MINOR.PATCH` with optional
   prerelease/build), rejecting malformed tags before anything is built.
2. **Check out the tagged commit** — `actions/checkout` at `github.ref`,
   never a branch head — and verify the resolved commit matches the tag.
3. **Run the full quality gates** in the same job. The release does not
   assume some earlier workflow passed on some earlier commit; it re-runs
   them against the exact source being packaged.
4. **Build** the matrix, **generate** SHA-256 checksums, **verify** them.
5. **Publish** a GitHub Release with the artifacts attached, using
   `CHANGELOG.md`'s existing section rather than a generated commit dump.

Permissions: `contents: write` on the release job only, because creating a
release requires it. Nothing else — no `packages: write`, no `id-token:
write`. `ci.yml` and `nightly.yml` remain `contents: read`.

The build steps run before the publish step, so a target that fails to
compile fails the release without having published anything.

## Tests

- Every advertised target cross-compiled.
- Archives inspected for expected contents.
- Checksums generated and **verified** with `shasum -c`.
- The host-native binary executed, and `trustvian version` output checked
  against the actual git revision.
- `check-modules.sh` proven to fail on a violation, not merely to pass.
- Workflow YAML structurally parsed and its permissions asserted.

## Documentation

`docs/release-guide.md` (new, maintainer-facing, separate from
end-user docs), `docs/ROADMAP.md`, `CONTRIBUTING.md`, `CHANGELOG.md`.
`README.md` gains no milestone status.

## Acceptance Criteria

1. Task 039 verified from implementation.
2. v0.9 roadmap reflects the real sequence with 040 task-filed.
3. Module topology documented; every module classified public vs
   repository-internal, with reasons.
4. The processor's local-`replace` question explicitly resolved and
   recorded, not left implicit.
5. No published module depends on a developer-local path.
6. Module consistency is automatically checked, and the check demonstrably
   fails on violation.
7. Every advertised binary target builds.
8. Archives have deterministic names and contain only intended files.
9. SHA-256 checksums generated and verified.
10. A release can be built locally with no credentials, tag, or upload.
11. Release automation targets the exact tagged commit and validates SemVer.
12. `ci.yml`/`nightly.yml` stay `contents: read`; the release workflow
    takes only `contents: write`.
13. Nothing published: no release, tag, artifact, or image.
14. No SBOM, scanner, or signing scope added.
15. Root, processor (`GOWORK=off`), and examples gates pass; PostgreSQL
    and Compose regressions pass.
16. Release documentation exists; `README.md` remains evergreen.


## Corrective pass (PR #52)

The first implementation of this task failed CI on the `Module
consistency` step while passing on every developer machine. The cause was
in the check, not the repository:

```
expected: v0.8.0 is a real version, so the processor's floor is truthful
actual:   "FAIL: requires root v0.8.0, which is not an existing tag"
```

`actions/checkout` performs a shallow clone and **fetches no tags by
default**. `git rev-parse --verify refs/tags/v0.8.0` therefore found
nothing in the CI workspace, and the script concluded the version did not
exist — about a version that is published, tagged, and resolvable. The
error message was worse than the failure, because it asserted something
false.

Two fixes, because there were two problems:

1. **The invariant was implemented as the wrong question.** It now asks
   whether the version *exists*, answering from a local tag when one is
   available and from the module proxy otherwise. When neither can answer —
   a tagless checkout with no network — it reports an inconclusive
   *environment* as its own distinct error rather than passing or blaming
   the version. A check that silently skips is not a check.
2. **CI was not giving the check what it needs.** The root job now sets
   `fetch-tags: true`, so the offline path is the normal one and the proxy
   fallback is a safety net rather than a per-run network dependency.

`scripts/check_modules_test.go` pins all of this with local fixtures, so
the regression cannot return: a valid development state passes offline, and
an unverifiable version fails with a message that names the cause instead
of the version.

One fact was proven rather than assumed while investigating: with the
`replace` removed and `go mod tidy` run, the processor **builds against the
published `v0.8.0` from the proxy**. The release therefore contains every
root API the processor uses, and the `replace` exists to develop against
*unreleased* root changes — not because any published version is
insufficient. That distinction is now documented in the release guide.

**No release-mode variant was added.** This task's model has one published
module, and nested modules that are not published, so there is no invariant
that is stricter at release time than during development — `release.yml`
runs the same check. A mode split would be complexity in advance of a
lifecycle that does not exist yet; the trigger to add one is promoting a
nested module to a published path.
