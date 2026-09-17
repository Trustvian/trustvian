#!/usr/bin/env bash
#
# Tests for image-name.sh.
#
#   ./scripts/test-image-name.sh
#
# The case this suite exists for is the first one below: the GitHub owner is
# `Trustvian`, the OCI repository must be `trustvian`. That mismatch failed
# the v0.9.0-rc.2 release at its first container step. Deterministic, no
# network, no Docker.

set -euo pipefail

cd "$(dirname "$0")/.."

readonly SCRIPT="./scripts/image-name.sh"
readonly CANONICAL="ghcr.io/trustvian/trustvian-collector"

pass_count=0
fail_count=0

# ok <name> <expected> [owner-argument]
ok() {
    local name="$1" want="$2"
    shift 2

    local got status
    set +e
    got="$(env -u GITHUB_REPOSITORY_OWNER "$SCRIPT" "$@" 2>&1)"
    status=$?
    set -e

    if [ "$status" -ne 0 ]; then
        printf 'FAIL  %s: exit %d\n%s\n' "$name" "$status" "$got" >&2
        fail_count=$((fail_count + 1))
        return
    fi
    if [ "$got" != "$want" ]; then
        printf 'FAIL  %s: got %q, want %q\n' "$name" "$got" "$want" >&2
        fail_count=$((fail_count + 1))
        return
    fi
    printf 'ok    %s\n' "$name"
    pass_count=$((pass_count + 1))
}

# rejects <name> <owner-argument>
rejects() {
    local name="$1" owner="$2"

    local got status
    set +e
    got="$(env -u GITHUB_REPOSITORY_OWNER "$SCRIPT" "$owner" 2>&1)"
    status=$?
    set -e

    if [ "$status" -eq 0 ]; then
        printf 'FAIL  %s: accepted %q and printed %q\n' "$name" "$owner" "$got" >&2
        fail_count=$((fail_count + 1))
        return
    fi
    printf 'ok    %s\n' "$name"
    pass_count=$((pass_count + 1))
}

# --- the rc.2 regression ----------------------------------------------
ok "GitHub owner Trustvian becomes lowercase" "$CANONICAL" "Trustvian"
ok "an already-lowercase owner is unchanged"  "$CANONICAL" "trustvian"
ok "mixed case is normalized"                 "ghcr.io/mixed-case/trustvian-collector" "MiXeD-Case"

# --- how the workflows call it ----------------------------------------
# No argument: GitHub's own environment variable, exactly as a runner sets it.
got="$(GITHUB_REPOSITORY_OWNER=Trustvian "$SCRIPT")"
if [ "$got" = "$CANONICAL" ]; then
    echo "ok    GITHUB_REPOSITORY_OWNER=Trustvian is normalized"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  GITHUB_REPOSITORY_OWNER=Trustvian: got %q, want %q\n' "$got" "$CANONICAL" >&2
    fail_count=$((fail_count + 1))
fi

# Nothing set at all: a local `make container-build` still works.
ok "defaults to the project organization" "$CANONICAL"

# --- the invariant, stated directly -----------------------------------
for owner in Trustvian TRUSTVIAN trustvian MiXeD-Case; do
    got="$("$SCRIPT" "$owner")"
    if printf '%s' "$got" | grep -q '[A-Z]'; then
        printf 'FAIL  no uppercase in output: owner %q produced %q\n' "$owner" "$got" >&2
        fail_count=$((fail_count + 1))
    fi
done
echo "ok    no owner spelling produces an uppercase repository"
pass_count=$((pass_count + 1))

# The result must be a legal OCI repository reference.
got="$("$SCRIPT" Trustvian)"
if printf '%s' "$got" | grep -Eq '^[a-z0-9.:-]+(/[a-z0-9]+([._-][a-z0-9]+)*)+$'; then
    echo "ok    output is a legal OCI repository reference"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  output is a legal OCI repository reference: %q\n' "$got" >&2
    fail_count=$((fail_count + 1))
fi

# --- refusals ---------------------------------------------------------
rejects "an owner with a space is refused"       "bad owner"
rejects "an owner with a slash is refused"       "owner/extra"
rejects "an owner with punctuation is refused"   'owner!'
rejects "an empty explicit owner is refused"     ""

# --- the canonical name is what the documentation promises ------------
if grep -rqF "$CANONICAL" docs/supply-chain.md; then
    echo "ok    canonical name matches docs/supply-chain.md"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  docs/supply-chain.md does not mention %s\n' "$CANONICAL" >&2
    fail_count=$((fail_count + 1))
fi

echo
if [ "$fail_count" -gt 0 ]; then
    printf 'image-name tests: %d passed, %d FAILED\n' "$pass_count" "$fail_count" >&2
    exit 1
fi
printf 'image-name tests: %d passed\n' "$pass_count"
