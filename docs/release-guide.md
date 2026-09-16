# Release Guide

Maintainer-facing. For using Trustvian, start at the
[README](../README.md); for contributing, see
[CONTRIBUTING.md](../CONTRIBUTING.md).

## Module publication model

This repository contains three Go modules and publishes exactly one. That
distinction is load-bearing and easy to get wrong, so it is written down
here and enforced by `scripts/check-modules.sh`.

| Module | Path | Published | Distribution |
|---|---|---|---|
| root | `github.com/Trustvian/trustvian` | **Yes** | Go module + release binaries |
| processor | `trustvian-processor` | No | Built from a clone |
| examples | `trustvian-examples` | No | Read and run in place |

**The root module is the product.** It carries the engine, the public API
(`event`, `config`, `alert`, and the root package), and the `trustvian`
CLI. It is tagged `vX.Y.Z` and must never contain a `replace` directive:
consumers ignore a dependency's replaces, so one in a published `go.mod`
means the module builds differently for everyone else than it does here —
a failure that only appears after the tag is public.

**The processor and examples modules are repository-internal**, and not
merely "not published yet". Their module paths — `trustvian-processor`,
`trustvian-examples` — are not resolvable: `go get trustvian-processor`
fails with *"malformed module path: missing dot in first path element"*.
Neither has ever been tagged. Both carry `replace
github.com/Trustvian/trustvian => ../`, which is exactly right for a
module built from this repository rather than fetched from a proxy.

Each exists as a separate module for a reason that has nothing to do with
publishing:

- **processor** keeps the heavy OpenTelemetry Collector dependency tree out
  of the root module's graph. `processor/README.md` documents running it
  with `go run ./cmd/trustvian-collector` from a clone.
- **examples** is a module that *cannot* import `internal/*`, which is what
  makes it a real proof that the public API is sufficient for an outside
  consumer.

### Promoting the processor

The processor is not consumable by anyone who has not cloned this
repository. That is a deliberate current state, not an oversight — no
external consumer exists and nothing documents a path for one.

The trigger to revisit is concrete: someone wanting to build a Collector
containing this processor with `ocb` / `otelcol-builder`, which requires a
resolvable module path. Promoting it then means:

1. Rename the module path to `github.com/Trustvian/trustvian/processor`.
2. Remove `replace github.com/Trustvian/trustvian => ../`.
3. `require github.com/Trustvian/trustvian vX.Y.Z` at a **released**
   version — so the root module must be tagged first. This ordering is not
   optional: a nested module cannot require an unreleased parent.
4. Tag the nested module as `processor/vX.Y.Z`. Go derives a nested
   module's version from a tag prefixed with its directory; a root `vX.Y.Z`
   tag does **not** version it.
5. Keep local development working — most simply through `go.work`, which
   already lists all three modules, rather than a replace in the published
   `go.mod`.

`scripts/check-modules.sh` fails if the path becomes resolvable while the
local replace is still present, so a half-finished promotion cannot merge
quietly.

## Preparing a release

### 1. Dry run first

```bash
make release-dry-run
```

Builds every advertised target, archives them, and generates and verifies
checksums — with no tag, no credentials, and no upload. Run this before
creating a tag; a target that fails to compile should be found now, not
after a tag is public. CI also runs this on every push.

### 2. Confirm the gates pass

```bash
make check                 # gofmt, vet, build, race
make check-modules         # module publication invariants
make integration-postgres  # PostgreSQL integration and stress tiers
```

The release workflow re-runs all of these against the tagged commit, so
this step is for fast feedback rather than trust.

### 3. Prepare the notes

Release notes come from [`CHANGELOG.md`](../CHANGELOG.md), not from a
generated commit dump. Rename the `## Unreleased` heading to the version
being released — the changelog's own convention is that this rename
happens only once a real tag exists, so it is part of releasing, not part
of preparing.

To control the release body exactly, put it in `release-notes.md` at the
repository root before tagging; the workflow uses it when present.

### 4. Choose the version

Semantic versioning, matching the compatibility promise in `CHANGELOG.md`.
The workflow rejects any tag that is not `vMAJOR.MINOR.PATCH` with an
optional prerelease or build suffix, so `v0.9.0` and `v0.9.0-rc.1` are
valid and `0.9` or `v0.9` are not.

### 5. Tag and push

Work lands on `develop` and reaches `main` by pull request; every release
tag so far sits on the resulting merge commit on `main`. Follow that:

```bash
git checkout main && git pull origin main
git tag -a v0.9.0 -m "Trustvian v0.9.0"
git push origin v0.9.0
```

Pushing the tag triggers `.github/workflows/release.yml`.

## What the automation does

| Step | Behavior |
|---|---|
| Trigger | Push of a tag matching `v*`. Never a branch push. |
| Tag validation | Rejects non-SemVer tags before building anything. |
| Source | Checks out `github.ref` — the tagged commit — and **verifies** `HEAD` equals the commit the tag points at. |
| Gates | Re-runs module consistency, format, vet, tests, race, PostgreSQL integration, and the processor module with `GOWORK=off`, against the tagged source. |
| Artifacts | `scripts/release-build.sh` — the same script `make release-dry-run` runs. |
| Checksums | SHA-256 manifest, generated and verified. |
| Publish | Creates a **draft** GitHub Release with the archives and `checksums.txt` attached. |

The release is a draft on purpose: a human reviews the notes against the
changelog and presses publish. That is the last cheap moment to catch a
wrong version or an incomplete changelog.

If any target fails to build, the script exits non-zero and the publish
step never runs — there is no partial release.

**Permissions.** `ci.yml` and `nightly.yml` are `contents: read`. The
release job takes `contents: write`, which is what creating a release
requires, and nothing more — no `packages: write`, no `id-token: write`.
Container publishing and signing are a later milestone slice and will bring
their own scopes when they arrive.

## Artifacts

Only the `trustvian` CLI is packaged. The processor's binaries belong to a
non-published module, and libraries are distributed as Go modules rather
than as binaries.

| OS | Arch | Archive |
|---|---|---|
| linux | amd64 | `trustvian_<version>_linux_amd64.tar.gz` |
| linux | arm64 | `trustvian_<version>_linux_arm64.tar.gz` |
| darwin | amd64 | `trustvian_<version>_darwin_amd64.tar.gz` |
| darwin | arm64 | `trustvian_<version>_darwin_arm64.tar.gz` |
| windows | amd64 | `trustvian_<version>_windows_amd64.zip` |

Each archive holds the binary, `LICENSE`, and `README.md` — nothing else.

Built with `CGO_ENABLED=0` (every dependency is pure Go, so the matrix
cross-compiles from one runner and the binaries carry no libc dependency)
and `-trimpath` (no build-machine paths embedded). There is no `-ldflags`
version injection: `trustvian version` reads Go's own build information,
which records the module version, VCS revision, commit time, and whether
the tree was dirty.

**Byte-for-byte reproducibility is not claimed.** The build avoids the
obvious sources of nondeterminism, but this has not been tested, and an
untested reproducibility claim is worse than none.

## Verifying a published release

```bash
# Checksums
curl -sLO https://github.com/Trustvian/trustvian/releases/download/v0.9.0/checksums.txt
curl -sLO https://github.com/Trustvian/trustvian/releases/download/v0.9.0/trustvian_v0.9.0_linux_amd64.tar.gz
sha256sum -c checksums.txt --ignore-missing

# The binary identifies the commit it was built from
tar -xzf trustvian_v0.9.0_linux_amd64.tar.gz
./trustvian_v0.9.0_linux_amd64/trustvian version

# The Go module resolves at the tag
cd "$(mktemp -d)" && go mod init probe
go get github.com/Trustvian/trustvian@v0.9.0
```

Signature verification is not available yet; artifact signing is a later
slice of `v0.9`.

## After the release

- Rename `## Unreleased` in `CHANGELOG.md` to the released version.
- Update milestone status in [`docs/ROADMAP.md`](ROADMAP.md).
- Merge `main` back into `develop`.
- Leave [`README.md`](../README.md) alone — it is deliberately evergreen and
  carries no version status.
