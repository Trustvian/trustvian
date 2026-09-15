package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SchemaVersion is the database schema version this package reads and
// writes. It is deliberately independent of both the Trustvian release
// version and config.StorageSchemaVersionV1 (the *configuration* schema)
// — the same decoupling every other schema version in this codebase
// already practices, and for the same reason: a release may ship without
// touching the database layout, and a layout change may land without a
// release bump.
//
// Bump this only when the persisted representation changes in a way a
// previous version's loader could misread. `Migrate` refuses to run
// against a database whose recorded version differs, rather than
// guessing — mirroring store.FileStore's own refusal to read an unknown
// fileSnapshotVersion instead of silently misparsing it.
const SchemaVersion = 1

// Table names, exported because operational inspection through plain
// SQL is an explicit goal of choosing PostgreSQL at all (see
// docs/ROADMAP.md § v0.8 and docs/storage-guide.md § Inspecting state) —
// a tool or runbook querying these should name them from here rather
// than duplicating the literal.
//
// They are compile-time constants, never interpolated from
// caller-supplied data. Every *value* reaches the database as a bound
// parameter (see postgres.go); these two identifiers are the only parts
// of any statement assembled from Go strings. See docs/SECURITY.md §
// Storage configuration and production persistence.
const (
	BaselineTable      = "trustvian_baseline"
	SchemaVersionTable = "trustvian_schema_version"
)

// Unexported aliases keep the SQL literals below readable.
const (
	baselineTable = BaselineTable
	versionTable  = SchemaVersionTable
)

// migrationAdvisoryLockKey serializes concurrent Migrate calls across
// processes. An arbitrary but fixed 64-bit constant, namespaced to this
// project by construction (there is no registry of advisory-lock keys in
// PostgreSQL; collision with another application using the same literal
// would merely serialize two unrelated migrations, never corrupt
// either).
const migrationAdvisoryLockKey int64 = 0x7275737476696E00 // "trustvin\0"

// createBaselineTable stores one row per baseline.Key.
//
// `baseline` (jsonb) is the **authoritative** state: it holds the
// complete, marshalled baseline.Baseline, using the identical
// encoding/json representation store.FileStore already persists. That
// reuse is the point — it makes PostgreSQL and FileStore structurally
// equivalent rather than equivalent by careful hand-matching, and it
// means a future Baseline field is persisted by both automatically,
// with no risk of this backend silently dropping state FileStore keeps.
//
// Every other column is **derived from that jsonb and exists only for
// operator inspectability** (see docs/ROADMAP.md § v0.8's queryability
// rationale): an operator can answer "which actors does Trustvian know
// about, how much has it learned, and when did it last update?" with
// plain SQL and no JSON decoding. They are never read back into a
// Baseline, so they cannot become a second, drifting source of truth.
// Writes recompute them from the same value they write to `baseline`,
// inside the same transaction.
//
// Deliberately NOT normalized into per-fingerprint / per-transition /
// per-delegator tables. Baseline is a bounded, self-contained value
// keyed by {ActorID, Environment}; splitting its internal maps across
// tables would (a) reshape the domain model to suit SQL, which ADR 0018
// rules out, (b) turn every single-row atomic update into a multi-table
// write needing its own consistency argument, and (c) invite the
// unbounded-history table this milestone explicitly rejects. There is no
// query today that normalization would serve, and jsonb remains
// queryable in SQL if one appears.
const createBaselineTable = `
CREATE TABLE IF NOT EXISTS ` + baselineTable + ` (
	actor_id          text        NOT NULL,
	environment       text        NOT NULL,
	baseline          jsonb       NOT NULL,
	schema_version    integer     NOT NULL,
	fingerprint_count integer     NOT NULL,
	observation_count bigint      NOT NULL,
	last_observed     timestamptz,
	updated_at        timestamptz NOT NULL,
	PRIMARY KEY (actor_id, environment)
)`

// createVersionTable holds exactly one row: the schema version this
// database was initialized at. A table rather than a comment or a
// pg_catalog trick so it is obvious to an operator reading the schema,
// and so Migrate can assert against it transactionally.
const createVersionTable = `
CREATE TABLE IF NOT EXISTS ` + versionTable + ` (
	version    integer     NOT NULL PRIMARY KEY,
	applied_at timestamptz NOT NULL DEFAULT now()
)`

// ErrSchemaVersionMismatch is returned when the database was
// initialized by a different schema version than this build expects.
// It is deliberately fatal rather than auto-upgrading: silently
// rewriting a layout written by another version is how state gets
// corrupted. A *newer* recorded version is the dangerous direction —
// an older binary must never mutate state whose layout it does not
// understand — and an older recorded version has no upgrade path to
// take while SchemaVersion is still 1.
var ErrSchemaVersionMismatch = errors.New("store/postgres: database schema version mismatch")

// ErrAmbiguousSchemaState reports a database whose schema metadata
// cannot be interpreted with confidence, as distinct from one whose
// version is simply wrong. Two cases produce it, both found by task
// 036's hardening pass rather than reasoned about in advance:
//
//   - The baseline table holds rows but the version table is empty.
//     Before task 036 this was silently treated as a fresh database and
//     stamped with the current SchemaVersion — meaning an operator who
//     restored a partial backup, or ran `DELETE FROM
//     trustvian_schema_version`, could have an older binary adopt state
//     written by a newer one. "No recorded version" is only safe to
//     interpret as "new database" when there is also no data.
//   - The version table holds more than one row. The version column is
//     a primary key, so several *different* versions can coexist, and
//     reading one with `LIMIT 1` and no ordering picked arbitrarily
//     between them — a database marked version 99 was observed being
//     accepted because a leftover version 1 row was read instead.
//
// Both fail closed. Recovery is an operator decision (restore a
// consistent backup, or set the version table to the single correct
// value); guessing on Trustvian's side risks corrupting learned state,
// which is exactly what a version check exists to prevent.
var ErrAmbiguousSchemaState = errors.New("store/postgres: ambiguous schema metadata")

// Migrate brings an empty database up to SchemaVersion and verifies an
// already-initialized one matches. It is:
//
//   - idempotent — running it repeatedly is a no-op after the first;
//   - transactional — the DDL and the version row commit together, so a
//     failure never leaves tables without a recorded version;
//   - safe under concurrent startup — a transaction-scoped advisory lock
//     serializes racing processes, and the second one observes the first
//     one's committed version rather than re-creating anything;
//   - fail-closed — an unrecognized recorded version aborts with
//     ErrSchemaVersionMismatch instead of being upgraded or ignored, and
//     metadata that cannot be interpreted at all aborts with
//     ErrAmbiguousSchemaState rather than being guessed at. "Absent
//     version" counts as fresh only when the baseline table is also
//     empty; see ErrAmbiguousSchemaState for why that distinction
//     matters.
//
// Automatic initialization is chosen over requiring an operator to run
// DDL by hand because it is the smallest safe OSS experience: one
// connection string and the store works. The cost is that the runtime
// role needs table-creation rights on first run — documented in
// docs/storage-guide.md, along with how to split migration from runtime
// privileges for deployments that care.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store/postgres: begin migration: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once Commit succeeds

	// Held until this transaction ends, so two processes starting
	// simultaneously cannot both run the DDL-and-insert sequence.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("store/postgres: acquire migration lock: %w", err)
	}

	if _, err := tx.Exec(ctx, createVersionTable); err != nil {
		return fmt.Errorf("store/postgres: create version table: %w", err)
	}
	if _, err := tx.Exec(ctx, createBaselineTable); err != nil {
		return fmt.Errorf("store/postgres: create baseline table: %w", err)
	}

	// Count first, rather than reading one row and inferring from
	// pgx.ErrNoRows. The count distinguishes the three states that
	// matter — none, exactly one, more than one — where a `LIMIT 1` read
	// collapses "none" and "several" into "whatever came back", which is
	// how both ErrAmbiguousSchemaState cases used to slip through.
	var versionRows int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+versionTable).Scan(&versionRows); err != nil {
		return fmt.Errorf("store/postgres: read schema version: %w", err)
	}

	switch {
	case versionRows > 1:
		return fmt.Errorf("%w: %s holds %d rows, expected exactly 1 — cannot determine the database's schema version",
			ErrAmbiguousSchemaState, versionTable, versionRows)

	case versionRows == 0:
		// No recorded version. Safe to treat as a fresh database *only* if
		// it is genuinely empty — otherwise this is data of unknown
		// provenance and stamping it would be a silent adoption.
		var baselineRows int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+baselineTable).Scan(&baselineRows); err != nil {
			return fmt.Errorf("store/postgres: count existing baselines: %w", err)
		}
		if baselineRows > 0 {
			return fmt.Errorf("%w: %s holds %d baseline row(s) but %s is empty — refusing to assume this data matches schema version %d",
				ErrAmbiguousSchemaState, baselineTable, baselineRows, versionTable, SchemaVersion)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO `+versionTable+` (version) VALUES ($1)`, SchemaVersion); err != nil {
			return fmt.Errorf("store/postgres: record schema version: %w", err)
		}

	default:
		var recorded int
		if err := tx.QueryRow(ctx, `SELECT version FROM `+versionTable).Scan(&recorded); err != nil {
			return fmt.Errorf("store/postgres: read schema version: %w", err)
		}
		if recorded != SchemaVersion {
			return fmt.Errorf("%w: database is at version %d, this build expects %d", ErrSchemaVersionMismatch, recorded, SchemaVersion)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store/postgres: commit migration: %w", err)
	}
	return nil
}
