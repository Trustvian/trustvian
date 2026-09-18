#!/usr/bin/env bash
#
# Tests for check-pr-title.sh.
#
#   ./scripts/test-check-pr-title.sh
#
# Deterministic, offline, no dependencies. Every title below is one of the
# worked examples in docs/COMMIT_CONVENTION.md, so this suite fails if the
# script and the document ever disagree.

set -euo pipefail

cd "$(dirname "$0")/.."

readonly SCRIPT="./scripts/check-pr-title.sh"

pass_count=0
fail_count=0

accepts() {
    local title="$1" out status
    set +e
    out="$(PR_TITLE= "$SCRIPT" "$title" 2>&1)"
    status=$?
    set -e
    if [ "$status" -ne 0 ]; then
        printf 'FAIL  should accept %q (exit %d)\n%s\n' "$title" "$status" "$out" >&2
        fail_count=$((fail_count + 1))
        return
    fi
    printf 'ok    accepts  %s\n' "$title"
    pass_count=$((pass_count + 1))
}

rejects() {
    local title="$1" out status
    set +e
    out="$(PR_TITLE= "$SCRIPT" "$title" 2>&1)"
    status=$?
    set -e
    if [ "$status" -eq 0 ]; then
        printf 'FAIL  should reject %q\n' "$title" >&2
        fail_count=$((fail_count + 1))
        return
    fi
    printf 'ok    rejects  %s\n' "$title"
    pass_count=$((pass_count + 1))
}

# --- valid: every type, with and without a scope ----------------------
accepts "feat: add notification routing"
accepts "feat(store): add PostgreSQL baseline persistence"
accepts "fix(postgres): restore readiness after a reconnect"
accepts "security(alert): reject unsafe callback targets"
accepts "perf(anomaly): reduce baseline lookup allocations"
accepts "refactor(store): isolate persistence initialization"
accepts "test(postgres): cover recovery after a database outage"
accepts "docs: document the commit convention"
accepts "ci(release): verify the container provenance"
accepts "build(otel): update OpenTelemetry dependencies"
accepts "chore: remove obsolete development scripts"

# --- valid: breaking changes ------------------------------------------
accepts "feat(config)!: replace legacy storage configuration"
accepts "feat!: replace the legacy storage configuration"

# --- valid: scope spellings this repository actually uses --------------
accepts "fix(store): retry the connection after recovery"
accepts "docs(governance): define agent merge restrictions"
accepts "ci(pr-title): validate the conventional prefix"
accepts "feat(cli): add a json output flag"

# --- invalid: the anti-patterns ---------------------------------------
rejects "Update README"
rejects "fixed bug"
rejects "changes"
rejects "WIP"
rejects "final"
rejects "final2"
rejects "fix stuff"
rejects "feat: Added new feature."
rejects "FEAT: add feature"
rejects "feature: add feature"

# --- invalid: near misses ---------------------------------------------
rejects "feat(store) add persistence"        # no colon
rejects "feat:add persistence"               # no space after the colon
rejects "feat(Store): add persistence"       # scope must be lowercase
rejects "feat(): add persistence"            # empty scope
rejects "feat: "                             # empty summary
rejects "feat: a"                            # summary too short to say anything
rejects "docs: document the convention."     # trailing period
rejects ": add persistence"                  # no type
rejects "chore(deps)(extra): update"         # malformed scope

# --- the error message has to be usable -------------------------------
out="$("$SCRIPT" "feature: add feature" 2>&1 || true)"
if printf '%s' "$out" | grep -q 'is not a valid type'; then
    echo "ok    names the invalid type"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  expected an invalid-type message, got:\n%s\n' "$out" >&2
    fail_count=$((fail_count + 1))
fi

out="$("$SCRIPT" "feat: Added new feature" 2>&1 || true)"
if printf '%s' "$out" | grep -q 'lowercase'; then
    echo "ok    explains the lowercase rule"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  expected a lowercase message, got:\n%s\n' "$out" >&2
    fail_count=$((fail_count + 1))
fi

out="$("$SCRIPT" "docs: document the convention." 2>&1 || true)"
if printf '%s' "$out" | grep -q 'period'; then
    echo "ok    explains the trailing-period rule"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  expected a trailing-period message, got:\n%s\n' "$out" >&2
    fail_count=$((fail_count + 1))
fi

# --- a missing title is a usage error, not a silent pass --------------
set +e
out="$(env -u PR_TITLE "$SCRIPT" 2>&1)"
status=$?
set -e
if [ "$status" -eq 2 ]; then
    echo "ok    a missing title exits 2"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  missing title: exit %d\n%s\n' "$status" "$out" >&2
    fail_count=$((fail_count + 1))
fi

# --- a long but valid title passes, with a note -----------------------
long="feat(store): add persistence so learned baselines survive a collector restart cleanly"
set +e
out="$("$SCRIPT" "$long" 2>&1)"
status=$?
set -e
if [ "$status" -eq 0 ] && printf '%s' "$out" | grep -q 'over the'; then
    echo "ok    a long valid title passes with a note"
    pass_count=$((pass_count + 1))
else
    printf 'FAIL  long title: exit %d\n%s\n' "$status" "$out" >&2
    fail_count=$((fail_count + 1))
fi

# --- the documented examples must all be valid ------------------------
# The document is the specification; if an example in it fails this script,
# one of the two is wrong and the run should say so.
doc_failures=0
while IFS= read -r example; do
    [ -n "$example" ] || continue
    if ! "$SCRIPT" "$example" >/dev/null 2>&1; then
        printf 'FAIL  docs/COMMIT_CONVENTION.md example rejected: %q\n' "$example" >&2
        doc_failures=$((doc_failures + 1))
    fi
done < <(sed -n '/^## Examples$/,/^## Anti-Patterns$/p' docs/COMMIT_CONVENTION.md |
    grep -E "^(feat|fix|security|perf|refactor|test|docs|ci|build|chore)" || true)

if [ "$doc_failures" -eq 0 ]; then
    echo "ok    every example in docs/COMMIT_CONVENTION.md is accepted"
    pass_count=$((pass_count + 1))
else
    fail_count=$((fail_count + doc_failures))
fi

echo
if [ "$fail_count" -gt 0 ]; then
    printf 'pr-title tests: %d passed, %d FAILED\n' "$pass_count" "$fail_count" >&2
    exit 1
fi
printf 'pr-title tests: %d passed\n' "$pass_count"
