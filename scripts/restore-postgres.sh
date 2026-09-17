#!/usr/bin/env bash
#
# Restore a Trustvian PostgreSQL backup into a NEW, EMPTY database.
#
#   createdb trustvian_restore_20260917                     # operator's action
#   PGHOST=db.internal PGUSER=trustvian PGPASSFILE=~/.pgpass PGSSLMODE=verify-full \
#     ./scripts/restore-postgres.sh --backup /secure/backups/trustvian-20260917T0400Z \
#                                   --target-db trustvian_restore_20260917
#
# Restore is the dangerous half of recovery, so this script is deliberately
# conservative. It will NOT:
#
#   - create, drop, or rename any database;
#   - restore into a database that holds any table, schema, or other session;
#   - restore into the database PGDATABASE names (usually the live one);
#   - proceed without a matching checksum — there is no bypass flag;
#   - point Trustvian at the result, or change anything about traffic.
#
# The target database is named with --target-db and nothing else; it is never
# inferred. Connection settings otherwise come from the libpq environment, as
# for backup-postgres.sh.
#
# What "verified" means here is STRUCTURAL: the archive restored completely in
# one transaction, both Trustvian tables and their primary keys exist, the
# schema-version row is present exactly once and matches the manifest, and
# every stored baseline is a JSON object. Whether the running release accepts
# that schema is decided by Trustvian itself at startup (its Migrate step) —
# deliberately not re-implemented here, so there is one schema authority. See
# docs/operations.md § Restore for the remaining steps: start Trustvian against
# the target, wait for /readyz, and verify learned state.
#
# On ANY failure after the restore has begun, the target is QUARANTINED
# (ALLOW_CONNECTIONS false, set from the `postgres` or `template1` maintenance
# database). A failed single-transaction restore leaves the target empty, and
# an empty database is indistinguishable from a brand-new deployment:
# Trustvian would initialize it and silently start learning from zero.
# Refusing connections makes that mistake fail closed instead. Drop the
# quarantined database once you have read the error.

set -euo pipefail

readonly DUMP_FILE="trustvian.dump"
readonly MANIFEST_FILE="MANIFEST"
readonly SUMS_FILE="SHA256SUMS"
readonly MANIFEST_FORMAT="trustvian-postgres-backup"
readonly MANIFEST_FORMAT_VERSION="1"
readonly BASELINE_TABLE="trustvian_baseline"
readonly VERSION_TABLE="trustvian_schema_version"

usage() {
    cat >&2 <<'EOF'
usage: restore-postgres.sh --backup <backup-directory> --target-db <new-empty-database>

The target database must already exist and be empty. Connection comes from
the libpq environment (PGHOST, PGPORT, PGUSER, PGPASSFILE/PGPASSWORD,
PGSSLMODE). See docs/operations.md.
EOF
    exit 2
}

fail() {
    printf 'restore-postgres: ERROR: %s\n' "$1" >&2
    exit 1
}

log() {
    printf 'restore-postgres: %s\n' "$1"
}

backup=""
target=""

while [ $# -gt 0 ]; do
    case "$1" in
        --backup)
            [ $# -ge 2 ] || usage
            backup="$2"
            shift 2
            ;;
        --target-db)
            [ $# -ge 2 ] || usage
            target="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            printf 'restore-postgres: unknown argument: %s\n' "$1" >&2
            usage
            ;;
    esac
done

[ -n "$backup" ] || usage
[ -n "$target" ] || usage

# ---------------------------------------------------------------------
# Preconditions that need no database.
# ---------------------------------------------------------------------

# A plain identifier only. This keeps the name out of any quoting question
# (it reaches SQL and the pg_restore command line), and rules out passing a
# connection string disguised as a database name.
[[ "$target" =~ ^[A-Za-z_][A-Za-z0-9_]{0,62}$ ]] ||
    fail "--target-db must be a plain database name (letters, digits, underscore; max 63)"

if [ -n "${PGDATABASE:-}" ] && [ "$PGDATABASE" = "$target" ]; then
    fail "--target-db '$target' is the database PGDATABASE names — most likely the active one. Restore into a new, empty database."
fi

[ -d "$backup" ] || fail "backup directory '$backup' does not exist"
for f in "$DUMP_FILE" "$MANIFEST_FILE" "$SUMS_FILE"; do
    [ -f "$backup/$f" ] || fail "backup is incomplete: '$backup/$f' is missing"
done

if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$@"; }
elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$@"; }
else
    fail "required tool 'sha256sum' (or 'shasum') not found on PATH"
fi

# The checksum file must cover exactly the dump and the manifest. A file that
# lists something else (or nothing) would let `-c` succeed while verifying
# nothing that matters.
listed="$(awk '{f=$2; sub(/^\*/, "", f); print f}' "$backup/$SUMS_FILE" | sort | tr '\n' ' ')"
expected="$(printf '%s\n%s\n' "$DUMP_FILE" "$MANIFEST_FILE" | sort | tr '\n' ' ')"
[ "$listed" = "$expected" ] ||
    fail "$SUMS_FILE must list exactly $DUMP_FILE and $MANIFEST_FILE; refusing an artifact whose integrity cannot be established"

if ! (cd "$backup" && sha256 -c "$SUMS_FILE" >/dev/null 2>&1); then
    fail "checksum verification FAILED for '$backup' — the backup is corrupt or was modified. Refusing to restore it."
fi
log "checksums verified"

manifest_value() {
    awk -F= -v k="$1" '$1 == k { sub(/^[^=]*=/, ""); print; exit }' "$backup/$MANIFEST_FILE"
}

[ "$(manifest_value format)" = "$MANIFEST_FORMAT" ] ||
    fail "manifest is not a $MANIFEST_FORMAT manifest"
[ "$(manifest_value format_version)" = "$MANIFEST_FORMAT_VERSION" ] ||
    fail "unsupported manifest format_version '$(manifest_value format_version)' (this script reads $MANIFEST_FORMAT_VERSION)"
expected_schema="$(manifest_value trustvian_schema_version)"
[[ "$expected_schema" =~ ^[0-9]+$ ]] ||
    fail "manifest trustvian_schema_version '$expected_schema' is not a number"

for tool in pg_restore psql; do
    command -v "$tool" >/dev/null 2>&1 || fail "required tool '$tool' not found on PATH"
done

# ---------------------------------------------------------------------
# The target must be reachable, empty, and unused. None of these checks
# writes anything: a refusal here leaves the target exactly as it was,
# because it may be a database someone depends on.
# ---------------------------------------------------------------------

target_query() {
    PGDATABASE="$target" psql -w -X -q -A -t -v ON_ERROR_STOP=1 -c "$1"
}

if ! target_query 'SELECT 1' >/dev/null 2>&1; then
    detail="$(PGDATABASE="$target" psql -w -X -q -A -t -c 'SELECT 1' 2>&1 >/dev/null | tail -n 2 || true)"
    fail "cannot connect to target database '$target' (create it first with createdb): ${detail:-no detail}"
fi

objects="$(target_query "
SELECT count(*) FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
   AND n.nspname NOT LIKE 'pg_toast%'
   AND n.nspname NOT LIKE 'pg_temp_%'" | tr -d '[:space:]')"
schemas="$(target_query "
SELECT count(*) FROM pg_namespace
 WHERE nspname NOT IN ('public', 'pg_catalog', 'information_schema')
   AND nspname NOT LIKE 'pg_toast%'
   AND nspname NOT LIKE 'pg_temp_%'" | tr -d '[:space:]')"
if [ "$objects" != "0" ] || [ "$schemas" != "0" ]; then
    fail "target database '$target' is not empty ($objects relation(s), $schemas extra schema(s)); refusing to restore over existing data"
fi

sessions="$(target_query "
SELECT count(*) FROM pg_stat_activity
 WHERE datname = current_database() AND pid <> pg_backend_pid()" | tr -d '[:space:]')"
[ "$sessions" = "0" ] ||
    fail "target database '$target' has $sessions other session(s) — something is using it. Refusing to restore into a database in use."

# ---------------------------------------------------------------------
# From here on, a failure leaves the target unusable on purpose.
# ---------------------------------------------------------------------

quarantine() {
    # PostgreSQL refuses to disallow connections to the database a session is
    # connected to, so this runs from a maintenance database instead: the
    # conventional `postgres`, falling back to `template1`, which every
    # cluster has. It needs ownership of the target, which the operator who
    # created it has. If it cannot be applied, say so loudly.
    local maint err=""
    for maint in postgres template1; do
        if err="$(PGDATABASE="$maint" psql -w -X -q -A -t -v ON_ERROR_STOP=1 \
            -c "ALTER DATABASE \"$target\" WITH ALLOW_CONNECTIONS false" 2>&1 >/dev/null)"; then
            printf 'restore-postgres: target database %s has been QUARANTINED (ALLOW_CONNECTIONS false) so nothing can start against it.\n' "$target" >&2
            printf 'restore-postgres: RESTORE FAILED — %s is NOT a valid Trustvian database. Drop it: dropdb %s\n' "$target" "$target" >&2
            return
        fi
    done
    printf 'restore-postgres: WARNING: could not quarantine %s (%s) — DO NOT point Trustvian at it.\n' "$target" "$(printf '%s' "$err" | tail -n 1)" >&2
    printf 'restore-postgres: RESTORE FAILED — %s is NOT a valid Trustvian database. Drop it: dropdb %s\n' "$target" "$target" >&2
}

restore_fail() {
    printf 'restore-postgres: ERROR: %s\n' "$1" >&2
    quarantine
    exit 1
}

log "restoring into '$target' (single transaction)"

errfile="$(mktemp)"
trap 'rm -f "$errfile"' EXIT

# --single-transaction: the restore commits entirely or not at all, so a
#   failure can never leave a half-restored Trustvian schema behind.
# --exit-on-error: stop at the first error instead of skipping it.
# --no-owner / --no-acl: objects belong to the restoring role, so a backup
#   taken under one role name restores cleanly under another. Re-apply any
#   runtime-role grants afterwards (docs/operations.md § Roles).
if ! pg_restore -w --exit-on-error --single-transaction --no-owner --no-acl \
    --dbname="$target" "$backup/$DUMP_FILE" 2>"$errfile"; then
    detail="$(tail -n 5 "$errfile" 2>/dev/null || true)"
    restore_fail "pg_restore failed; nothing was committed: ${detail:-no detail}"
fi

# ---------------------------------------------------------------------
# Structural verification.
# ---------------------------------------------------------------------

check() {
    # $1 description, $2 query, $3 expected value
    local got
    got="$(target_query "$2" 2>/dev/null | tr -d '[:space:]')" ||
        restore_fail "verification query failed: $1"
    [ "$got" = "$3" ] || restore_fail "verification failed: $1 (got '$got', want '$3')"
}

check "Trustvian tables present" \
    "SELECT (to_regclass('$BASELINE_TABLE') IS NOT NULL)::int + (to_regclass('$VERSION_TABLE') IS NOT NULL)::int" "2"
check "primary keys present" \
    "SELECT count(*) FROM pg_constraint WHERE contype = 'p' AND conrelid IN (to_regclass('$BASELINE_TABLE'), to_regclass('$VERSION_TABLE'))" "2"
check "exactly one schema-version row" \
    "SELECT count(*) FROM $VERSION_TABLE" "1"
check "schema version matches the manifest" \
    "SELECT version FROM $VERSION_TABLE" "$expected_schema"
check "every stored baseline is a JSON object" \
    "SELECT count(*) FROM $BASELINE_TABLE WHERE jsonb_typeof(baseline) <> 'object'" "0"
check "derived schema_version consistent with the recorded version" \
    "SELECT count(*) FROM $BASELINE_TABLE WHERE schema_version <> (SELECT version FROM $VERSION_TABLE)" "0"

rows="$(target_query "SELECT count(*) FROM $BASELINE_TABLE" | tr -d '[:space:]')"
observations="$(target_query "SELECT coalesce(sum(observation_count), 0) FROM $BASELINE_TABLE" | tr -d '[:space:]')"

log "RESTORE VERIFIED (structural)"
log "  target database:  $target"
log "  schema version:   $expected_schema"
log "  baselines:        $rows"
log "  observations:     $observations"
log "  backup created:   $(manifest_value created_at)"
log "next (operator): start Trustvian against '$target', confirm it starts and /readyz returns 200,"
log "then verify learned state before any cutover. See docs/operations.md § Restore."
