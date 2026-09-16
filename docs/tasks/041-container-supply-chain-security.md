# 041 — Container Supply Chain & Security

**Milestone:** v0.9 — Operational Readiness · **Depends on:**
[040](040-release-artifacts-and-module-consistency.md) (the release
identity, tag validation, and gates this extends) ·
**Blocks:** 045 (`v0.9` stabilization) · **Third slice of `v0.9`.**

## Objective

Turn Trustvian's locally built reference container into a reproducible,
verifiable, securely distributed OSS container artifact, with the minimal
supply-chain controls appropriate to `v0.9`.

## What exists today

`v0.8` task 037 built a container deliberately scoped to local use:

| | State before this task |
|---|---|
| Image definition | `deployments/docker-compose/Dockerfile.collector` — reference build, `golang:1.27-alpine` → `alpine:3.22`, non-root uid 10001, `CGO_ENABLED=0` |
| Registry | none — nothing has ever been published |
| Architectures | whatever the developer's machine is |
| `.dockerignore` | **absent** — the build context includes `.git` and `dist/` |
| SBOM | none |
| Vulnerability scanning | none |
| Signing / provenance | none |

Task 037 said so explicitly: *"This is a REFERENCE DEPLOYMENT BUILD IMAGE,
not the official published Trustvian image."* This slice supplies the
official one without disturbing that.

## Which executable is the official image

**The Collector** (`processor/cmd/trustvian-collector`).

It is the only long-lived network service this repository produces — the
`trustvian` CLI is a batch tool, already distributed as a binary and via
`go install` by task 040. A container exists to run a service, and the
Collector-with-processor is the service that performs behavioral analysis
on live telemetry.

That the Collector lives in the repository-internal `processor` module is
not an obstacle: a container ships a compiled binary, not a Go module, so
the module's non-resolvable path (see [task
040](040-release-artifacts-and-module-consistency.md) § Module publication
model) has no bearing on shipping its binary.

## Image identity and tags

```text
ghcr.io/trustvian/trustvian-collector
```

GHCR because the repository lives on GitHub and `GITHUB_TOKEN` can publish
there with no long-lived credential to store or rotate. The name is
explicit about *what* runs, so a future second image (a CLI image, say)
needs no renaming of this one.

Tags on a release:

| Tag | Mutable | Purpose |
|---|---|---|
| `vX.Y.Z` | **No** | The deployment contract. Immutable, one specific build. |
| `X.Y` | Yes | Convenience for tracking patches within a minor. |
| `latest` | Yes | Convenience only, never a deployment contract. |

A digest (`@sha256:...`) is the strongest reference and is what verification
examples use. Prereleases (`v0.9.0-rc.1`) get **only** the immutable tag —
moving `latest` or `X.Y` to a release candidate would hand it to everyone
tracking a floating tag.

## Scope

```text
Dockerfile                          official Collector image (repository root)
.dockerignore                       build-context hardening
.github/workflows/release.yml       + container build, scan, publish, sign, attest
.github/workflows/ci.yml            + govulncheck, + container build check
Makefile                            container-build, container-scan, sbom, vulncheck
docs/supply-chain.md                maintainer + verifier documentation
docs/SECURITY.md                    supply-chain section, distinct from runtime security
```

## Non-Goals

Owned by later slices, deliberately absent here: health and readiness
endpoints, graceful shutdown, self-observability or Prometheus metrics,
resource limits (042/043); backup, restore, and upgrade documentation
(044); Kubernetes, Helm, HA, Control Plane, MCP security, runtime
behavioral provenance, numeric baselines, and new detectors (not `v0.9`).

Also excluded: **no image is published, no tag created, no release made**
during this task. A second release process — task 041 extends task 040's
single release identity rather than competing with it.

**Supply-chain provenance here is not Trustvian's future "Runtime Identity
& Provenance" behavioral feature.** This is *build* provenance: which
commit and workflow produced an artifact. That feature is about *runtime*
actor provenance. Same word, unrelated concerns.

## Runtime image

`gcr.io/distroless/static-debian12:nonroot`.

Chosen for concrete requirements, not smallness:

- **CA certificates.** The Collector dials PostgreSQL, potentially with
  `sslmode=verify-full`, and may export telemetry over TLS. `scratch` has
  no trust store, which would silently break TLS verification. Distroless
  `static` includes one.
- **No shell, no package manager.** The Collector's operational model is
  config-in / logs-out with all state in PostgreSQL. Nothing about
  operating it requires shelling into the container, so the attack surface
  a shell represents buys nothing.
- **Non-root by construction** — the `:nonroot` variant runs as uid 65532
  with no `adduser` step to get wrong.
- **`static` rather than `base`** because `CGO_ENABLED=0` makes the binary
  static; there is no libc to provide.

The trade-off is accepted knowingly: `docker exec` cannot get a shell.
Debugging is by logs, by the Collector's own diagnostics, and — if a
filesystem question ever arises — by running the same tag with a
debug-shelled base locally. That is documented rather than discovered.

## Architectures

`linux/amd64` and `linux/arm64`, both verified to build. Nothing else is
advertised: the remaining buildx platforms have no evidence behind them,
and an architecture nobody has built is a claim, not a feature.

## Supply chain

| Concern | Tool | Why this one |
|---|---|---|
| SBOM | BuildKit's built-in SBOM attestation (`--sbom=true`), SPDX | Platform-standard, attached to the image as an attestation, no extra tool in the release path |
| Build provenance | BuildKit provenance attestation (`--provenance=mode=max`) | Same: standard, low-maintenance, no custom JSON |
| Go dependencies | `govulncheck` | Symbol-level reachability. On this repository it already proves its worth: the processor's dependency graph carries advisories that its code cannot reach, which a graph-only scanner would report as findings needing triage |
| Container packages | Trivy | One scanner, not three. Covers the OS/base layer that `govulncheck` does not |
| Signing | Cosign keyless (Sigstore, GitHub OIDC) | No key material to store or rotate |

Two scanners, covering disjoint ground: `govulncheck` for Go code
reachability, Trivy for base-image packages. No second Go scanner is added
for coverage that `govulncheck` already provides better.

### Vulnerability gate policy

- **Fails a release:** `CRITICAL` or `HIGH` **with a fix available**, in
  the container image.
- **Reported, does not fail:** anything unfixed, and `MEDIUM`/`LOW`. An
  unfixed advisory cannot be actioned by rebuilding, so blocking on it
  converts an upstream problem into an inability to ship a security fix of
  our own.
- **`govulncheck` fails on any *reachable* vulnerability**, at any
  severity. Reachability is the signal; severity is secondary once the code
  actually calls the vulnerable symbol.
- **Exceptions** are recorded in `docs/supply-chain.md` with the finding,
  the reason, its scope, and the condition for review. No blanket
  time-unbounded ignores.

One exception exists at the time of writing: `GO-2026-5932` in
`golang.org/x/crypto`, an indirect dependency of the processor module, with
no fixed version available and not reachable from Trustvian code. Two
sibling advisories in the same module (`GO-2026-6355`, `GO-2026-6354`) were
cleared by upgrading to `v0.56.0` as part of this task.

## Release integration

One release identity. Task 040's workflow already validates the tag,
verifies the checkout is the tagged commit, and runs the gates; the
container job **needs** that job, so it inherits the same verified source
rather than rebuilding trust:

```text
tag push
  → validate SemVer, verify tagged commit, run gates      (040)
  → build binaries, checksums, draft GitHub Release       (040)
  → build multi-arch image from the same commit           (041)
  → scan; gate on fixable CRITICAL/HIGH                   (041)
  → publish to GHCR                                       (041)
  → sign + attach SBOM/provenance attestations            (041)
```

Scan **before** publish, so a known-bad image is never pushed. Signing and
attestation come after publish because both reference an image by digest,
which only exists once pushed.

If the container job fails after the binary release succeeded, the GitHub
Release remains a **draft** — task 040 already creates drafts — so an
incomplete release is never presented as finished. That is the mechanism
answering "no partial success ambiguity": publication is a human action
taken after seeing all jobs.

## Permissions

| Workflow / job | Permissions | Why |
|---|---|---|
| `ci.yml` | `contents: read` | Unchanged. Publishes nothing. |
| `nightly.yml` | `contents: read` | Unchanged. |
| `release.yml` → release | `contents: write` | Creates the GitHub Release (040). |
| `release.yml` → container | `contents: read`, `packages: write`, `id-token: write` | Push to GHCR; OIDC token for keyless signing. |

`id-token: write` is confined to the job that signs. No long-lived registry
credential: `GITHUB_TOKEN` authenticates to GHCR.

## Local verification

A maintainer must be able to exercise this without publishing:

```bash
make container-build    # multi-arch build, no push
make sbom               # SBOM to dist/, from the built image
make vulncheck          # govulncheck across both modules
make container-scan     # Trivy against the local image
```

Signing is genuinely CI-only — keyless Cosign needs a GitHub OIDC token
that exists only inside a workflow run. That is documented, not worked
around with a local key.

## Compose compatibility

`deployments/docker-compose/` keeps building from source. A developer must
not need a published image to run the repository, and the reference
deployment's whole point is demonstrating the path from source. The
supply-chain guide documents how to substitute the official image *after*
releases exist, without changing the default.

## Tests

- Both advertised architectures built.
- The image inspected for: non-root user, no shell, expected entrypoint,
  and that it contains no Go toolchain, source, or `.git`.
- SBOM generated, **parsed**, and checked for expected components — not
  merely confirmed to exist.
- `govulncheck` run on both modules; findings reported, not hidden.
- Trivy run against the built image; findings reported by severity.
- Workflow YAML parsed and every job's permissions asserted.
- Build context verified to exclude `.git` and `dist/`.

## Acceptance Criteria

1. Task 040 independently verified DONE before starting.
2. Official image identity, registry, and tag strategy defined and
   documented, with an immutable `vX.Y.Z` tag as the deployment contract.
3. The container is built from the exact release commit, inheriting task
   040's tag verification.
4. `linux/amd64` and `linux/arm64` both build; nothing else advertised.
5. Runtime image is minimal for stated reasons, runs non-root, and grants
   no added capabilities.
6. Build context hardened; no `.git`, `dist/`, or local env files.
7. SBOM generated and validated by parsing.
8. `govulncheck` implemented for Go dependencies; Trivy for container
   packages; no redundant scanner.
9. Vulnerability gate policy documented, including every live exception.
10. Publishing prepared, occurring only on a trusted tag event; `ci.yml`
    and `nightly.yml` stay `contents: read`; `packages: write` and
    `id-token: write` confined to the jobs needing them; no long-lived
    registry credential.
11. Signing and provenance implemented, or deferred with justification.
12. Supply-chain and security documentation written, keeping build
    supply-chain distinct from runtime behavioral security.
13. Reference Compose deployment still builds from source and passes.
14. All prior gates pass: root, module consistency, processor
    `GOWORK=off`, examples, PostgreSQL, Compose.
15. No image published, no tag created, no release created; documentation
    claims no artifact that does not yet exist.
