# 038 — `v0.8` Stabilization & Release Gate

**Milestone:** v0.8 — Production Runtime & Storage · **Depends on:**
[034](034-production-store-contract-and-public-boundary.md),
[035](035-postgresql-store-implementation.md),
[036](036-store-durability-concurrency-and-migration-hardening.md),
[037](037-reference-docker-compose-deployment.md) · **Fifth and final
slice of `v0.8`.**

## Objective

Decide, with evidence, whether `v0.8.0` is fit to release — and correct
anything that would be frozen badly by shipping it.

This is not a feature slice. Its output is a decision, a synchronized set
of documents, and a release handoff.

## Why a gate slice exists

Four slices built production persistence. Each verified its own work. None
of them asked the question this one asks: *is the whole thing coherent, and
is the public surface something we want to support for years?*

Two categories of problem only surface at this point:

- **Documentation drift.** Each slice updated the docs it touched. Claims
  made in earlier slices, in sections later slices did not read, quietly
  became false. A reader hitting a contradiction cannot tell which half is
  true.
- **API regret.** Public API added mid-milestone has not been released yet,
  so it can still be changed for free. After the tag it cannot. This is the
  last moment where "we should not have exported that" costs nothing.

## Findings and corrections

### 1. A public sentinel error nothing could ever return (HIGH — fixed)

`config.ErrStorageTypeNotImplemented` was exported by task 034 for the
state "a backend Trustvian recognizes but this release cannot build."
Task 035 implemented PostgreSQL, which was the only value ever in that
state. The error was left exported and unreachable — no code path returns
it, and none can.

Its own doc comment defended keeping it on the grounds that "it is exported
API that a caller may already check with `errors.Is`." That reasoning was
wrong: it was introduced in this same unreleased milestone and has never
appeared in a released version, so no caller can be checking it.

**Removed.** The decisive argument is asymmetry: *adding* an exported error
later is backward-compatible, *removing* one is not. Keeping it would have
frozen a permanent trap — a sentinel users could write dead `errors.Is`
checks against — in exchange for nothing. If a future backend ever needs
the distinction it drew (a real backend this release does not ship, versus
a typo'd backend name), re-adding an error is free.

Two doc comments in `config/validate.go` that described the
now-removed state were corrected at the same time, as was
[ADR 0018](../../../adr/0018-production-store-boundary-and-postgresql-direction.md),
which records the original decision and now carries an update note.

### 2. Documentation contradictions (DOC-ONLY — fixed)

Three statements contradicted code truth:

| Location | Stale claim | Correction |
|---|---|---|
| `README.md` | "PostgreSQL, the next `v0.8` slice; the store *boundary* now exists, **the backend does not**" | Removed; both PostgreSQL and the reference deployment landed in `v0.8` |
| `docs/ROADMAP.md` | Task 034's narrative said, in present tense, that `type: postgres` **is** recognized-but-unimplemented | Rewritten as history, noting 035 implemented it and 038 removed the error |
| `examples/persistent-baseline/README.md` | Described PostgreSQL as unimplemented and named `TestPublicConfigUnimplementedBackendFailsClosed`, a test renamed in 035 | Updated to the real test names and current behavior |

The README case is the one that mattered: the same file described
PostgreSQL as implemented in one section and nonexistent in another.

### 3. A stray grammar slip in the roadmap's status line (LOW — fixed)

"…[035], **and** [036], **and** [037] are done" — corrected while
rewriting that block for the status transition.

## What this slice verified

Everything below was re-run *after* the corrections above, per this task's
own rule that a READY verdict may not rest on pre-change results.

- **Milestone truth from implementation, not task files.** Task 037 was
  gated by inspecting the deployment itself: the Compose file, the pinned
  PostgreSQL version, the named volume, `smoke-test.sh`'s executable bit in
  the index, the processor's use of `config.CompileStorage`, and the
  absence of any `pgx`/`sql.Open` in the processor.
- **The external-consumer path**, from a module that cannot import
  `internal/*`: public config → `CompileStorage` → `WithStore` → Engine,
  against a real database.
- **Every PostgreSQL correctness gate** from tasks 035/036 — contract,
  contention, first-write race, isolation, rollback, cancellation, lock-wait
  cancellation, pool exhaustion, leak checks, schema compatibility,
  migration atomicity, corruption handling, bounded state.
- **Durability across a verified real PostgreSQL restart**, not just a
  client restart.
- **Backward compatibility**: `NewEngine()` with no options still works
  with no infrastructure; all three backends produce identical learned
  state, scores, and decisions.
- **The full Compose workflow from a clean clone** — no `.git`, no build
  artifacts, no developer paths — including fail-closed behavior and
  `down -v` teardown.
- **Standalone module boundaries**: `GOWORK=off` for the processor, and the
  examples module independently.

## Non-Goals

- **No new capability.** The only code change is a deletion.
- **No `v0.9` work**: no CI, no official image publishing, no SBOM, no
  signing, no scanning, no Kubernetes, no Helm, no HA.
- **No commit, push, tag, or GitHub release.** Those are human-controlled
  and deliberately outside this task.
- **No schema redesign.** The `v0.8` schema was reviewed for forward
  evolution and left alone.

## Freeze reviews

### Public API — STABLE

`config.StorageConfig`, `config.PostgresStorageConfig`,
`config.FileStorageConfig`, `config.CompileStorage`, `config.LoadStorage`,
`config.LoadStorageFile`, the `StorageType*` constants, and the storage
error sentinels were each reviewed for whether they are worth supporting
after `v0.8.0`. All are — with the one removal described above.

Two shapes were examined and deliberately left as they are:

- **`store.Store` has no `Close`.** Lifecycle is an optional, type-asserted
  `io.Closer`, matching the existing `store.Freezer` precedent. Widening
  the port would force every implementation and caller to change for a
  capability only database-backed stores need.
- **`Store.Get` has no error return.** A failed read reports "no baseline",
  which is fail-safe in the direction that matters (the actor reads as
  unfamiliar, raising anomaly rather than suppressing it), never writes, and
  is surfaced loudly by the very next `Observe`. Widening the port to
  improve reporting in an already-fail-safe case would be a breaking change
  looking for a justification.

### Configuration schema — STABLE

`version`/`type`/`file`/`postgres`, and within `postgres` the `dsn`,
`max_connections`, and `connect_timeout_seconds` fields. Both numeric knobs
treat zero as "use the default", so the schema has room to change defaults
without breaking documents. `version: v1` is explicit, so a `v2` is
available if the shape ever needs to change incompatibly.

### Database schema — can evolve

One version row, checked on every startup; unknown-newer, unknown-older,
missing-with-data, and ambiguous states all fail closed. Authoritative
state lives in a single `jsonb` column, so adding a `Baseline` field needs
no DDL at all — which is what makes most plausible `v0.9` changes
schema-compatible by construction. A genuine layout change bumps
`SchemaVersion` and adds a migration step; the mechanism for that is
already tested for atomicity and concurrent-startup safety.

## Acceptance Criteria

1. Tasks 034–037 verified from implementation, not from DONE markers.
2. No BLOCKER and no HIGH finding outstanding.
3. Root, processor (`GOWORK=off`), and examples gates all pass —
   `gofmt`, `go vet`, `go test`, `go test -race`, benchmarks.
4. The full PostgreSQL integration and stress tiers pass against a real
   server, with zero lost updates.
5. Durability proven across a real database restart and a client restart.
6. Schema compatibility fails closed for newer, older, missing-with-data,
   and ambiguous metadata.
7. No credential leak; all SQL values parameterized; no raw-event
   warehouse; bounded state intact.
8. Backward compatibility confirmed: `InMemory` default, `FileStore`
   supported, `v0.7` configuration unchanged, behavioral semantics
   storage-independent.
9. The Compose reference deployment passes end to end from a clean clone,
   including fail-closed and teardown semantics.
10. Public API and configuration schema reviewed and classified; any
    genuine flaw corrected before release.
11. Documentation contradictions found and fixed; no file claims both that
    PostgreSQL exists and that it does not.
12. `CHANGELOG` `v0.8.0` entry and GitHub release notes prepared.
13. Roadmap says RELEASE READY and `v0.8.0` NOT YET SHIPPED — never
    SHIPPED before the tag exists.
14. No commit, push, tag, or release created.
