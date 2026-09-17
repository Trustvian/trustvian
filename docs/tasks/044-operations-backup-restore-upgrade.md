# 044 — Operations: Backup, Restore & Upgrade

**Milestone:** v0.9 — Operational Readiness · **Depends on:**
[042](042-runtime-health-readiness-graceful-shutdown.md) (readiness, the
operational gate after a restore or upgrade),
[043](043-self-observability-resource-safety.md) (metrics an operator watches
during one), [036](036-store-durability-concurrency-and-migration-hardening.md)
(the schema-compatibility rules an upgrade runs into) · **Blocks:** 045
(`v0.9` stabilization) · **Sixth slice of `v0.9`.**

## Objective

Give OSS operators a documented, testable, and safe procedure for protecting
Trustvian's learned behavioral state, restoring it after failure, and
upgrading between compatible releases — without silently losing or resetting
security state.

This is an operational-safety task. It adds no behavioral capability, no
storage backend, and no Trustvian-specific backup format.

## Scope decision against the roadmap

The `v0.9` capability-coverage table assigned three further rows to 044:
*security disclosure process*, *CONTRIBUTING.md / issue templates*, and
*production examples*. None of them concerns learned state, and bundling them
here would produce a task that cannot be reviewed as one capability. They are
**not** in this task; the roadmap records them as open items for 045's gate
to place, rather than silently dropping them.

## What the audit found

Verified from code, not assumed.

### Persisted state inventory

| State | Backend | Durable | Backup required | Reconstructable |
|---|---|---|---|---|
| Learned baselines (`baseline.Baseline` per `{actor_id, environment}` — fingerprints, volatile stats, hour activity, transitions, trigrams, delegators) | `trustvian_baseline.baseline` (jsonb) | Yes | **Yes** | **No** — relearning resets every actor to cold start |
| Derived inspection columns (`fingerprint_count`, `observation_count`, `last_observed`, `updated_at`, `schema_version`) | `trustvian_baseline` | Yes | Included with the row | Yes, from the jsonb — never read back |
| Actor state beyond the baseline | — | — | — | Nothing else is persisted: no events, decisions, alerts, or sessions |
| Freeze flags (`store.Freezer`) | InMemory / FileStore process memory; not implemented by PostgreSQL | **No** | No | Re-applied by the operator (by design, per-process) |
| Sequence state beyond the baseline | — | — | — | Only what lives inside `Baseline` (bounded, ADR 0010) |
| Policies | YAML files (`config.PolicyConfig`) or processor `policy:` block | Operator-owned files | **Yes — operator's config management** | From version control |
| Anomaly / storage / alert configuration | YAML files / Collector config | Operator-owned files | **Yes — operator's config management** | From version control |
| Database credentials (DSN) | Environment / secret manager | Operator-owned | Secret manager's own backup | Never in a Trustvian backup |
| Schema metadata | `trustvian_schema_version` (exactly one row) | Yes | **Yes** — restore without it fails closed (`ErrAmbiguousSchemaState`) | No |
| Migration metadata | Same row; `Migrate` has no other ledger | Yes | Included | No |
| `FileStore` state | One JSON file (`fileSnapshot`, version 1), atomic temp+rename writes | Yes | Yes, if used | No |
| InMemory state | Process memory | **No** | Impossible | No — lost on restart |

**Trustvian persists no policy in the database.** A database backup restores
*what was learned*, never *how decisions are made*; both are needed to
reproduce a decision.

### Write pattern (drives the consistency argument)

`postgres.Store.Observe` is one transaction touching **one row**:
`INSERT … ON CONFLICT DO NOTHING`, `SELECT … FOR UPDATE`, `UPDATE`, `COMMIT`.
Derived columns are written in the same statement as the jsonb. There are no
cross-row invariants: every row is self-contained. `Migrate` writes the
version row and DDL in one transaction, only when the database is fresh.

`pg_dump` reads the whole database in one `REPEATABLE READ` snapshot. Each
row therefore appears exactly as of the last commit before the snapshot, and
no row can be half-written. **An online backup is consistent for the current
schema; no downtime is required.** Observations committed after the snapshot
are not in the backup — that is the recovery point, not an inconsistency.

### Migration ownership

| Question | Answer (from `internal/store/postgres/schema.go`) |
|---|---|
| Who runs migrations? | The runtime, at store construction (`NewStore` → `Migrate`) |
| When? | Every startup, before the store is returned |
| Transactional? | Yes — DDL and version row commit together |
| Idempotent? | Yes |
| Version tracked? | `trustvian_schema_version`, exactly one row; `SchemaVersion = 1` |
| Concurrent startup? | Serialized by a transaction-scoped advisory lock |
| Failure semantics? | Startup fails; no store, no fallback to memory |
| Upgrade path between versions? | **None exists** — there has only ever been version 1; any other recorded version is `ErrSchemaVersionMismatch` |
| Dirty/partial migration possible? | No — the transaction commits entirely or not at all |
| `force` command? | None exists and none is added |

### Release history relevant to upgrade

Latest real release: **`v0.8.0`** (the first with a PostgreSQL store). Between
`v0.8.0` and this commit, nothing changed in `internal/baseline`,
`internal/features`, `internal/fingerprint`, `internal/store` (other than the
added `Ping`), `config`, or `event`; `SchemaVersion` is `1` in both. The
configuration change since `v0.8.0` is one **new optional** processor block
(`health:`, task 042). No field was removed or renamed.

### Tooling found

No backup or restore script, manifest, or checksum convention exists for
state. `scripts/release-build.sh` establishes SHA-256 as the project's
artifact-integrity convention (task 040). The reference Compose deployment
pins `postgres:17-alpine`; the CI database service is PostgreSQL 17; the
storage layer is tested against 17 (task 036).

## Contracts

### Backup

| | |
|---|---|
| Tool | `pg_dump` — PostgreSQL-native, nothing Trustvian-specific |
| Format | Custom archive (`--format=custom`): compressed, schema + data, restorable selectively, portable across machines |
| Contents | The whole database: both tables, their primary keys, and all rows. Not data-only |
| Online/offline | **Online.** One snapshot; the runtime keeps running |
| Wrapper | `scripts/backup-postgres.sh` — thin, validates, writes checksum and manifest |
| Connection | Standard libpq environment only (`PGHOST`, `PGPORT`, `PGUSER`, `PGDATABASE`, `PGPASSFILE`/`PGPASSWORD`, `PGSSLMODE`, `PGSERVICE`) — **never a DSN on a command line**, where process listings expose it |
| Preconditions | Output does not exist; `PGDATABASE` explicitly set; database carries exactly one Trustvian schema-version row |
| Output | A new directory: `trustvian.dump`, `MANIFEST`, `SHA256SUMS`; written under a temporary name and renamed into place only when complete |
| Integrity | SHA-256 over the dump and the manifest; archive table of contents checked for both tables' schema and data before success |
| Permissions | `umask 077`: directory `0700`, files `0600` |
| Overwrite | Never — an existing output path is refused |
| Manifest | Format and version, UTC timestamp, dump file and format, Trustvian schema version, optional operator-supplied Trustvian release, server and `pg_dump` versions. **No host, user, database name, DSN, or password** |

### Restore

| | |
|---|---|
| Target | An **existing, empty** database the operator created and names explicitly |
| Overwrite active DB | **Never.** A non-empty target, a target with other sessions, or a target equal to `PGDATABASE` is refused before anything is written |
| Creates / drops / renames databases | Never |
| Checksum | Required; mismatch refused; no bypass flag |
| Mechanism | `pg_restore --exit-on-error --single-transaction --no-owner --no-acl` |
| Structural verification | Both tables present, primary keys present, exactly one version row equal to the manifest, every stored baseline a JSON object, derived `schema_version` consistent |
| Compatibility verification | **Trustvian's own `Migrate` at startup** — the script does not re-implement the compatibility rules (no second schema authority) |
| Behavioral verification | Tested (below) and part of the documented drill |
| Failure | Exit non-zero, report `RESTORE FAILED`, and **quarantine** the target (`ALLOW_CONNECTIONS false`) so nothing — Trustvian included — can start against it |
| Automatic cutover | **None.** Pointing the runtime at the restored database is the operator's action |

The quarantine exists because of a finding, not as ornament: after a failed
single-transaction restore the target is *empty*, and an empty database is
indistinguishable from a new deployment — Trustvian would initialize it and
start learning from zero. Refusing connections turns "silently reset" into
"fails closed at startup".

### Recovery means

1. Learned behavioral state is preserved — proven by behavior, not row counts.
2. The schema is accepted by the target release's own `Migrate`.
3. Policy and configuration are restored from the operator's configuration
   management (not from the database).
4. The runtime reaches `/readyz` 200.
5. Analysis resumes without a baseline reset: new observations *add to* the
   restored state.

A successful `pg_restore` exit code alone is not recovery.

### Upgrade

```text
healthy old runtime (/readyz 200)
→ verified backup (checksum + archive TOC)
→ stop/drain old runtime (SIGTERM; readiness flips first — task 042)
→ deploy new binary or exact-version image
→ start: Migrate verifies/initializes schema — mismatch fails startup, never degrades
→ /livez 200, /readyz 200
→ confirm observation counts continue from pre-upgrade values
→ watch trustvian_analyses{outcome}, trustvian_observations{outcome}, latency (task 043)
```

Multiple replicas: stop all old replicas before starting new ones whenever
the target release changes `SchemaVersion`. For releases sharing a schema
version, a rolling replacement is safe (tested: old and new binaries against
the same database produce identical results).

### Rollback

Safe for every release pair:

```text
stop new runtime
→ restore the pre-upgrade backup into a new, empty database
→ deploy the previous exact version
→ /readyz 200, verify state
→ operator cutover
```

Observations learned by the new release after the backup are lost by this
path. That is the price of a rollback that cannot misread state.

**Binary-only downgrade** (old binary against the upgraded database) is safe
**only when both releases share `SchemaVersion`** — which is the case from
`v0.8.0` to this release and is tested. Across a schema-version change the
old binary refuses to start (`ErrSchemaVersionMismatch`); that refusal is
also tested, and is the reason the backup is mandatory.

A caveat recorded honestly: a future release that adds a `Baseline` field
*without* bumping `SchemaVersion` would make binary-only downgrade silently
drop that field on the next `Observe`, because JSON decoding ignores unknown
fields. The contract for future releases is therefore: **any change to the
persisted `Baseline` shape bumps `SchemaVersion`.**

### Compatibility matrix (`v0.x`)

| Upgrade | Supported | Evidence |
|---|---|---|
| `v0.8.0` → this release (same schema v1) | **Yes**, in place | Automated test from the real `v0.8.0` tag |
| Patch → patch within a minor | Yes, when `SchemaVersion` is unchanged | Schema check at startup |
| Previous minor → next minor | Only as that release's notes state | No multi-version migration exists yet |
| Skipping minors | **Not supported** until a release documents it | — |
| Downgrade, same schema version | Binary-only is safe | Tested for `v0.8.0` ↔ this release |
| Downgrade, different schema version | **Restore the pre-upgrade backup** | Old binary refuses the newer schema (tested) |
| `memory` store, any upgrade | No state survives — nothing to upgrade | — |
| PostgreSQL major version change | Via logical dump/restore into the new server; not tested beyond 17 | — |

Pre-`v1.0`, compatibility guarantees are narrower than after it. What is
not narrowed: no upgrade may silently reset or reinterpret learned state.

### InMemory and FileStore

- **InMemory:** no durable backup guarantee; a restart loses all learned
  state. Not changed by this task.
- **FileStore:** one JSON file written by atomic temp-file-plus-rename, so a
  plain file copy is always a complete snapshot. Back up with a file copy
  plus `sha256sum`; restore by stopping the process and replacing the file.
  Its `fileSnapshotVersion` check refuses unknown formats. Single-process by
  design, so no concurrent-writer semantics apply. No script is added — a
  copy command does not need a wrapper.

## Non-Goals

No custom backup format or JSON export. No backup scheduler, retention
daemon, S3/cloud uploader, or vendor dependency. No automatic restore into a
running database, no database creation/drop/rename, no DNS or load-balancer
changes, no automatic cutover. No self-updater, migration orchestrator, or
`force` command. No persistence for InMemory. No new metrics. No Kubernetes
or Helm. No RPO/RTO numbers. No Task 045 work, no release, no tag.

## Deliverables

| File | Purpose |
|---|---|
| `scripts/backup-postgres.sh` | Thin `pg_dump` wrapper: preconditions, checksum, manifest, permissions |
| `scripts/restore-postgres.sh` | Conservative `pg_restore` wrapper: checksum, fresh-target guard, structural verification, quarantine on failure |
| `scripts/backup_restore_test.go` | Script safety tests (always) and the DB-backed recovery, consistency, failure, and upgrade tests (opt-in) |
| `deployments/docker-compose/recovery-drill.sh` | End-to-end drill on the real runtime: backup under load → restore → cutover → `/livez`/`/readyz` → continued learning |
| `docs/operations.md` | The canonical operator runbook |
| CI | A focused PR job running the recovery and upgrade tests; the Compose drill nightly |

The reference deployment gains an explicit `health:` block (port 13133 on
loopback) and a `TRUSTVIAN_RUNTIME_POSTGRES_DB` variable, so the drill's
cutover is one explicit variable, not a rewritten config.

## Tests

**Always (no database):** missing arguments refused; existing backup output
refused; missing backup refused; checksum mismatch on dump and on manifest
refused; unlisted or missing checksum entries refused; invalid target name
refused; target equal to `PGDATABASE` refused.

**Opt-in (`TRUSTVIAN_TEST_BACKUP_RESTORE=1` + `TRUSTVIAN_TEST_POSTGRES_DSN`,
PostgreSQL client tools matching the server major):**

- *Behavioral recovery:* learn through the gated `Analyze`+`Observe` loop →
  backup → restore into a fresh database → a new Engine on the restored
  database produces identical baselines and identical Anomaly/Trust/Decision
  for probe events, which differ from a cold-start engine's (non-vacuous) →
  learning continues from the restored counts.
- *Backup during writes:* concurrent observers across many actors while
  `pg_dump` runs → restored database opens, every row decodes, derived
  columns agree with the jsonb, and each actor's restored count lies between
  what was acknowledged before and after the backup.
- *Failure injection:* bad connection (no credential in output); non-Trustvian
  database refused; non-empty target refused and untouched; target in use
  refused; truncated archive → `pg_restore` fails → target empty and
  quarantined; manifest/database schema mismatch → quarantined; newer schema
  version refused by Trustvian startup.
- *Upgrade (`TRUSTVIAN_TEST_UPGRADE_FROM_BINARY`):* the real `v0.8.0` CLI,
  built from its tag, learns a corpus into PostgreSQL → pre-upgrade backup →
  current code opens it in place and its baselines and decisions equal those
  current code learns from the same corpus → binary-only downgrade on the same
  schema gives identical analysis → rollback restore reproduces pre-upgrade
  analysis → a newer schema version is refused by both binaries.

**Drill (nightly):** `recovery-drill.sh` on the Compose stack.

## Acceptance Criteria

1. Specification frozen before implementation.
2. Persistent-state inventory documented; InMemory non-durability explicit.
3. Backup, restore, upgrade, rollback, and recovery-drill procedures
   documented in one runbook.
4. Backup uses `pg_dump` custom format; no custom format; schema + data.
5. Backup integrity verifiable by SHA-256; manifest contains no secret;
   artifacts not world-readable.
6. Restore targets a new, empty database; never overwrites or cuts over;
   checksum required with no bypass.
7. Restore verifies structure; Trustvian startup is the compatibility
   authority; learned behavioral state verified by behavior.
8. Corrupt backup rejected; failed restore never reported healthy and left
   quarantined.
9. Pre-upgrade backup is a documented prerequisite; upgrade preserves learned
   state (tested from `v0.8.0`); migration failure fails closed with no
   InMemory fallback.
10. Rollback limitations documented honestly, including when binary-only
    downgrade is safe.
11. Exact immutable versions in examples; no `latest` upgrade contract; binary
    and container upgrades both documented.
12. No fictional RPO/RTO.
13. Automated backup/restore and upgrade tests exist and run in CI.
14. All prior gates pass: root, `-race`, module consistency, processor
    `GOWORK=off`, examples, PostgreSQL tiers, task 042 health, task 043
    metrics, release dry-run, container build, vulnerability gates.
15. Documentation synchronized; `README.md` stays evergreen.

## Findings during implementation

**`ALTER DATABASE … ALLOW_CONNECTIONS false` cannot target the current
database.** The first quarantine implementation ran it while connected to the
target, and PostgreSQL rejected it (`cannot disallow connections for current
database`). The tests caught it: the failed target stayed connectable. The
script now issues it from the `postgres` maintenance database, falling back to
`template1`.

**The first atomicity test was vacuous.** It truncated the archive to
two-thirds, which cut into the table of contents, so `pg_restore` failed before
executing anything — and the test still passed with `--single-transaction`
removed. The damage now removes only the archive's tail, so the schema is
created before the failure; removing `--single-transaction` then fails the test
with "left 2 relation(s) behind".

**A Compose cutover set on one command does not persist.**
`TRUSTVIAN_RUNTIME_POSTGRES_DB=… docker compose up -d otel-collector` followed
by a plain `docker compose run demo-producer` recreated the collector on the
original database, because `run` re-evaluates the dependency without the
variable. The drill caught it (restored database at 40, not 80). The drill now
exports the variable, and the deployment README says to record it in `.env`.

## Verification

Local environment: PostgreSQL 17 (`postgres:17-alpine`), client tools 17.11 in a
Linux container, Go 1.27, Docker 29.8.0. The upgrade source is the `v0.8.0` CLI
built from its tag with `GOWORK=off`.

| Gate | Result |
|---|---|
| `gofmt -l` (root, processor, examples) | clean |
| Root `go vet`, `go build`, `go test`, `go test -race` | pass |
| `./scripts/check-modules.sh` | OK |
| Processor `GOWORK=off`: build, vet, test, `-race`, `go mod verify` | pass |
| Examples `GOWORK=off`: build, vet, test; no `internal/` imports | pass |
| `govulncheck` root / processor / examples | 0 / 0 reachable (known documented exception unreachable) / 0 |
| PostgreSQL integration + stress tier (`go test -race ./...` with DSN) | pass |
| Database restart durability | pass |
| Processor against PostgreSQL (health, shutdown, metrics, storage) | pass |
| Backup/restore/upgrade tier, `-race -count=2` | pass (6 tests × 2) |
| Mutations: no `--single-transaction`, no quarantine, checksum bypass, schema-only restore, no emptiness guard | each fails a test |
| `recovery-drill.sh` | 8/8 |
| `smoke-test.sh` (Compose changed) | 7/7 |
| `make release-dry-run` | 5 targets, checksums OK |
| Container build `linux/amd64` (loaded), `linux/arm64` | pass; nothing pushed |
| Trivy, fixable CRITICAL/HIGH | 0 |
| SBOM (SPDX 2.3, BuildKit attestation) | 151 packages |
| `go list -deps` pgx/OTel in `event`, `internal/features`, `internal/policy`, `internal/store`, root | 0 / 0 |

Recovery proof: three actors × 90 observations learned through the gated
`Analyze`/`Observe` loop → online backup (`0700`/`0600`, manifest without
connection detail) → restore into a new database → baselines byte-identical
after decoding, probe Anomaly/Trust/Decision/Explanation deep-equal, and
different from a cold engine → one more observation gives 91 on the restored
database while the source stays at 90.

Upgrade proof: `v0.8.0` `baseline build` → pre-upgrade backup → the current CLI's
`analyze` on the same database matches `v0.8.0`'s output exactly → stored
baselines equal those the current release learns from the same corpus →
`v0.8.0` still produces identical analysis afterwards (same schema) → the current
release keeps learning (+2) → a rollback restore gives `v0.8.0` its pre-upgrade
output → with the schema version set to 2, both binaries refuse to start.
