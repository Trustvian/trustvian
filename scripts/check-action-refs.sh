#!/usr/bin/env bash
#
# Verify that every GitHub Action referenced by this repository's workflows
# actually exists at the ref it names.
#
#   ./scripts/check-action-refs.sh
#
# This exists because of a real release failure. `v0.9.0-rc.1` passed every
# gate — including actionlint — and then died in job setup with:
#
#   Unable to resolve action `sigstore/cosign-installer@v4`,
#   unable to find version `v4`
#
# The YAML was valid and the action was real; only that *version* did not
# exist, because sigstore/cosign-installer publishes exact tags and no
# floating major. No linter checks that, because it needs a network lookup.
# This script is that lookup, and nothing more — it is deliberately not a
# dependency manager: it does not update, pin, or police versions.
#
# A ref resolves if it is a tag, a branch, or a full commit SHA in the
# action's repository. Anything unresolved fails the run.
#
# Authentication: uses `gh` when available, otherwise curl with GITHUB_TOKEN
# (unauthenticated requests work but are rate-limited to 60/hour).

set -euo pipefail

cd "$(dirname "$0")/.."

readonly WORKFLOW_DIR=".github/workflows"

fail_count=0

api() {
    # $1: API path. Prints the response body; non-zero exit means "not found"
    # or an API failure, which the caller distinguishes by the body.
    if command -v gh >/dev/null 2>&1; then
        gh api "$1" 2>/dev/null
    elif [ -n "${GITHUB_TOKEN:-}" ]; then
        curl -sf -H "Authorization: Bearer $GITHUB_TOKEN" \
            -H "Accept: application/vnd.github+json" \
            "https://api.github.com/$1"
    else
        curl -sf -H "Accept: application/vnd.github+json" \
            "https://api.github.com/$1"
    fi
}

resolves() {
    # $1: owner/repo, $2: ref. Tag, then branch, then commit SHA — the same
    # order the Actions runner resolves them in.
    local repo="$1" ref="$2"
    api "repos/$repo/git/ref/tags/$ref" >/dev/null 2>&1 && { echo "tag"; return 0; }
    api "repos/$repo/git/ref/heads/$ref" >/dev/null 2>&1 && { echo "branch"; return 0; }
    if [[ "$ref" =~ ^[0-9a-f]{40}$ ]]; then
        api "repos/$repo/commits/$ref" >/dev/null 2>&1 && { echo "commit"; return 0; }
    fi
    return 1
}

[ -d "$WORKFLOW_DIR" ] || {
    echo "check-action-refs: no $WORKFLOW_DIR directory" >&2
    exit 1
}

# One line per distinct owner/repo@ref, so an action used by five jobs costs
# one API lookup rather than five.
refs="$(grep -rhoE '^[[:space:]]*-?[[:space:]]*uses:[[:space:]]*[^[:space:]#]+' "$WORKFLOW_DIR" |
    sed -E 's/.*uses:[[:space:]]*//' | sort -u)"

[ -n "$refs" ] || {
    echo "check-action-refs: no 'uses:' references found — refusing to pass vacuously" >&2
    exit 1
}

while IFS= read -r use; do
    case "$use" in
        ./*|docker://*)
            # A local composite action or a container action: no ref to resolve.
            printf '  skip: %s (local or container action)\n' "$use"
            continue
            ;;
    esac

    if [[ "$use" != *@* ]]; then
        printf '  FAIL: %s (no @ref — a floating default branch is not a release input)\n' "$use" >&2
        fail_count=$((fail_count + 1))
        continue
    fi

    ref="${use##*@}"
    path="${use%@*}"
    # owner/repo, dropping any subdirectory path (owner/repo/sub@ref).
    repo="$(printf '%s' "$path" | cut -d/ -f1,2)"

    if kind="$(resolves "$repo" "$ref")"; then
        printf '  ok:   %s (%s)\n' "$use" "$kind"
    else
        printf '  FAIL: %s — ref does not exist in %s\n' "$use" "$repo" >&2
        fail_count=$((fail_count + 1))
    fi
done <<<"$refs"

if [ "$fail_count" -gt 0 ]; then
    printf '\ncheck-action-refs: %d unresolvable action reference(s).\n' "$fail_count" >&2
    printf 'A workflow using one fails at job setup, before any step runs.\n' >&2
    exit 1
fi

echo
echo "Action references: OK"
