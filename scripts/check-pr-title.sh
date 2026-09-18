#!/usr/bin/env bash
#
# Validate a pull request title against docs/COMMIT_CONVENTION.md.
#
#   ./scripts/check-pr-title.sh "feat(store): add baseline persistence"
#   PR_TITLE="docs: document the commit convention" ./scripts/check-pr-title.sh
#
# Trustvian squash-merges, so the pull request title becomes a commit subject
# in `main` permanently. That is the one subject line worth validating: branch
# commits are squashed away, but this one is history.
#
# Deliberately not a commit-message hook. Validating every intermediate commit
# would add friction to the part of the workflow that gets thrown away, and
# would not improve `main` at all.
#
# No network, no dependencies, no Node.js toolchain — this is a Go project.

set -euo pipefail

readonly TYPES='feat|fix|security|perf|refactor|test|docs|ci|build|chore'

# <type>(<scope>)!: <summary>
#
# The scope is optional and lowercase, `!` marks a breaking change, and the
# summary must start with a lowercase letter or digit so that a capitalized
# sentence ("Added new feature") is rejected rather than quietly accepted.
readonly PATTERN="^(${TYPES})(\([a-z0-9][a-z0-9-]*\))?!?: [a-z0-9].+$"

# The subject length the convention asks for. Advisory: a long subject is worth
# a second look, but it is not a defect worth blocking a merge over.
readonly SOFT_LIMIT=72

if [ $# -ge 1 ]; then
    title="$1"
else
    title="${PR_TITLE:-}"
fi

if [ -z "$title" ]; then
    echo "check-pr-title: no title given (pass an argument or set PR_TITLE)" >&2
    exit 2
fi

fail() {
    printf 'check-pr-title: %s\n\n' "$1" >&2
    printf '  title: %s\n\n' "$title" >&2
    cat >&2 <<EOF
Pull request titles follow docs/COMMIT_CONVENTION.md:

  <type>(<scope>): <imperative summary>

  type:    ${TYPES//|/, }
  scope:   optional, lowercase, e.g. (store), (postgres), (release)
  summary: imperative, lowercase, no trailing period

Examples:

  feat(store): add PostgreSQL baseline persistence
  fix(postgres): restore readiness after a reconnect
  security(alert): reject unsafe callback targets
  docs: document the commit convention
  feat(config)!: replace legacy storage configuration
EOF
    exit 1
}

# Order matters: report the most specific problem a reader can act on, rather
# than "does not match the pattern" for every kind of mistake.
case "$title" in
    *.) fail "the summary must not end with a period" ;;
esac

if ! printf '%s' "$title" | grep -Eq "$PATTERN"; then
    # Distinguish the two mistakes that are otherwise indistinguishable from
    # "malformed": a valid-looking type that is not one of ours, and a summary
    # that starts with a capital.
    if printf '%s' "$title" | grep -Eq '^[A-Za-z]+(\([^)]*\))?!?: '; then
        given="${title%%:*}"
        given="${given%%(*}"
        given="${given%!}"
        case "$given" in
            feat|fix|security|perf|refactor|test|docs|ci|build|chore)
                fail "the summary must begin with a lowercase letter or digit" ;;
            *)
                fail "$(printf '%q is not a valid type' "$given")" ;;
        esac
    fi
    fail "does not match <type>(<scope>): <summary>"
fi

if [ "${#title}" -gt "$SOFT_LIMIT" ]; then
    printf 'check-pr-title: note: %d characters, over the ~%d the convention suggests\n' \
        "${#title}" "$SOFT_LIMIT" >&2
fi

printf 'ok  %s\n' "$title"
