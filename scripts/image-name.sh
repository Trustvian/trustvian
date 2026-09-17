#!/usr/bin/env bash
#
# Print the canonical container image repository for this project.
#
#   ./scripts/image-name.sh            # owner from the environment, or trustvian
#   ./scripts/image-name.sh Trustvian  # explicit owner
#
#   -> ghcr.io/trustvian/trustvian-collector
#
# This exists because `v0.9.0-rc.2` failed here:
#
#   ERROR: failed to build:
#   invalid tag "ghcr.io/Trustvian/trustvian-collector:scan":
#   repository name must be lowercase
#
# The release workflow built the image name from
# ${{ github.repository_owner }}, which is the GitHub organization's display
# name — `Trustvian`, with a capital T. GitHub identity casing is not OCI
# repository casing: an OCI repository name must be lowercase, so every
# container step of the release failed on the first one that ran.
#
# One script, one answer. The build, the scan, the push, the Cosign subject,
# the SBOM/provenance subject, and the Makefile all resolve the repository
# through here, so it is not possible to scan one repository and sign
# another — which a per-step string would eventually allow.
#
# Tags are the caller's business; this prints the repository only. A tag may
# contain uppercase and SemVer punctuation (`v0.9.0-rc.2`), the repository
# may not.

set -euo pipefail

readonly REGISTRY="${TRUSTVIAN_IMAGE_REGISTRY:-ghcr.io}"
readonly IMAGE_BASENAME="trustvian-collector"

# Owner precedence: an explicit argument, then GitHub's own environment
# variable (set in every Actions run), then the project's own organization
# so that a local `make container-build` needs no environment at all.
#
# An argument that is present but empty is NOT treated as absent: a caller
# passing "$SOMEVAR" that turned out to be unset must get an error, not a
# silent fallback to a plausible default. Guessing is the failure mode this
# script exists to remove.
if [ $# -ge 1 ]; then
    owner="$1"
else
    owner="${GITHUB_REPOSITORY_OWNER:-trustvian}"
fi

# The normalization the release workflow was missing.
owner="$(printf '%s' "$owner" | tr '[:upper:]' '[:lower:]')"

# Validate rather than assume. An OCI path component is lowercase
# alphanumerics with single separators; anything else must fail loudly here
# instead of inside a build step whose error names a tag, not a cause.
if ! printf '%s' "$owner" | grep -Eq '^[a-z0-9]+([._-][a-z0-9]+)*$'; then
    printf 'image-name: %q is not a usable OCI repository component\n' "$owner" >&2
    exit 1
fi

image="${REGISTRY}/${owner}/${IMAGE_BASENAME}"

# Belt and braces: the whole reference must be free of uppercase. If a future
# edit reintroduces it — a registry host, a renamed basename — this fails
# here rather than in a release.
if printf '%s' "$image" | grep -q '[A-Z]'; then
    printf 'image-name: generated repository %q contains uppercase\n' "$image" >&2
    exit 1
fi

printf '%s\n' "$image"
