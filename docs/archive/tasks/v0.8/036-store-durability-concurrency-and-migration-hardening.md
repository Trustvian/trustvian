# 036 — Store Durability, Concurrency & Migration Hardening

**Milestone:** v0.8 — Production Runtime & Storage · **Depends on:**
[034](034-production-store-contract-and-public-boundary.md) (the
executable `Store` contract), [035](035-postgresql-store-implementation.md)
(the PostgreSQL backend this task hardens) · **Blocks:** 037 (reference
Docker Compose deployment), 038 (`v0.8` stabilization) · **Third slice of
`v0.8`.**

## Objective

Answer one question with evidence rather than argument:

> Can Trustvian safely use PostgreSQL as its production behavioral-state
> store under realistic concurrent and failure conditions?

Task 035 proved the backend *works*. This task proves it *holds* — under
contention, cancellation, exhausted pools, lost connections, restarted
databases, and unreadable schema metadata. It adds no backend, no feature,
and no configuration surface.

## Why

A storage backend that passes its own unit tests has demonstrated the
happy path. Production is mostly not the happy path: connections die
mid-transaction, two replicas start at the same moment, a client
disconnects while its transaction waits on a row lock, a restore leaves
metadata inconsistent. None of those appear in a green test run, and all
of them decide whether learned security state survives.

There is a second reason specific to this project. Every behavioral test
in the repository — `v0.5` policy, `v0.6` sequence analysis, `v0.7` agent
security, the learning-eligibility gate — runs against the in-memory
store. That entire body of evidence transfers to PostgreSQL only if the
backends are genuinely interchangeable. Until this task, that was an
assumption.

## What this task found

Two real defects in task 035's `Migrate`, both in the direction of
silently accepting state it should refuse:

1. **Missing version metadata with data present was treated as a fresh
   database.** `Migrate` read the version row, saw `pgx.ErrNoRows`, and
   stamped the current `SchemaVersion` — without checking whether the
   baseline table held rows. An operator who restored a partial backup, or
   ran `DELETE FROM trustvian_schema_version`, could have an older binary
   adopt state written by a newer one. "No recorded version" is only safe
   to read as "new database" when there is also no data.

2. **Ambiguous version metadata was resolved arbitrarily.** The version
   column is a primary key, so several *different* versions can coexist.
   The read used `LIMIT 1` with no `ORDER BY`, and a database marked
   version 99 was observed being accepted because a leftover version 1 row
   came back instead.

Both now fail closed with a new `ErrAmbiguousSchemaState`, distinct from
`ErrSchemaVersionMismatch` so an operator can tell "wrong version" from
"unreadable metadata". Recovery is deliberately an operator decision:
guessing is what these checks exist to prevent.

A third finding was a **test-validity** problem rather than a product
defect, recorded because it nearly produced a false guarantee: the
database-restart durability test passed against a restart command that
did nothing. It now reads `pg_postmaster_start_time()` before and after
and fails if the server did not actually restart — verified by pointing
it at `true` and confirming it fails.

## Scope

```text
internal/store/postgres/schema.go          Migrate: ambiguity checks (the only production change)
internal/store/postgres/hardening_test.go  failure, lifecycle, schema integrity
internal/store/postgres/stress_test.go     contention, restart durability
engine_storage_equivalence_test.go         backend equivalence + v0.5–v0.7 regressions
```

**One production change**, in `Migrate`. Everything else is evidence.
That ratio is the intended outcome of a hardening slice: if hardening had
required reworking `Observe`, the concurrency design would have been
wrong, not merely untested.

## Non-Goals

- **No new storage backend.** No Redis, SQLite, MySQL, CockroachDB,
  DynamoDB, MongoDB, Kafka, ClickHouse, or OpenSearch.
- **No redesign of the `Store` port, `Observe`, or the config boundary.**
  They held under every condition tested.
- **No retry framework, reconnect loop, or degraded mode.** See
  § Retries below for why none is needed.
- **No migration framework** (Flyway, goose, Atlas, migrate). The embedded
  transactional migration is sufficient and is now tested for atomicity.
- **No invented migration history.** `SchemaVersion` is still 1; no
  artificial v1→v2 upgrade was fabricated to exercise a path that does not
  exist. See § Migration upgrade path.
- **No Docker Compose reference deployment** (task 037), no Kubernetes,
  no Helm.
- **No OTel dependency in the store.** Operational metrics stay deferred.

## Hardening matrix

| Area | Coverage before 036 | Gap | Action |
|---|---|---|---|
| Same-key concurrency | Contract test, 8×25 | Modest scale | 32×100 × 3 rounds, exact count |
| First-write concurrency | 24 writers, one round | Modest scale | 96 writers × 5 rounds, + row-count check |
| Different-key isolation | 12 keys × 8 | No throughput evidence | 32 keys × 100, with logged rate comparison |
| Transaction rollback | **None** | Untested | Trigger-injected failure; prior baseline preserved |
| Client restart | Covered (035) | — | Retained; extended by equivalence tests |
| **Database restart** | **None** | Untested | Real server restart, self-verifying |
| Connection loss | **None** | Untested | `pg_terminate_backend`; acknowledged == stored |
| Startup unavailable | Covered (035) | — | Retained |
| Context cancellation | Cancelled-ctx only | No deadline case, no `errors.Is` | Table-driven; `errors.Is` asserted |
| **Lock-wait cancellation** | **None** | Untested | Blocked waiter cancelled; verified via `pg_locks` |
| Pool exhaustion | **None** | Untested | Pool of 1; bounded, context-aware |
| Connection leaks | **None** | Untested | Every failure path, pool of 1 |
| Close semantics | Doc claim only | Unverified | Idempotent close; post-close ops fail |
| Schema mismatch | Single case | Newer/older/missing/ambiguous untested | 7-case table; **2 defects fixed** |
| Migration concurrency | 2 racers | Modest | 12 simultaneous initializations |
| Migration failure atomicity | **None** | Untested | Aborted migration leaves nothing |
| Migration data preservation | **None** | Untested | Repeated re-init over populated DB |
| Corrupt stored state | **None** | Untested | Explicit error; **never silently reset** |
| Large bounded state | **None** | Untested | Past all caps; full round-trip compare |
| Backend equivalence | **None** | Assumed | State, decisions, poisoning, agent semantics |

## Concurrency, as it actually is

- **Isolation level: READ COMMITTED** (PostgreSQL's default; `Observe`
  sets none). This is deliberate and sufficient, not an oversight.
  Correctness comes from *explicit* row locking — `SELECT ... FOR UPDATE`
  — not from isolation level. REPEATABLE READ or SERIALIZABLE would add
  serialization-failure retries to handle, for a guarantee the row lock
  already provides. The weakest level that is correct with explicit
  locking is the right choice.
- **Row lock scope: exactly one row per transaction.** `Observe` locks
  `(actor_id, environment)` and nothing else.
- **Deadlock risk: structurally zero.** A deadlock requires two
  transactions holding locks and each waiting for the other's. `Observe`
  acquires exactly one row lock and never a second, so no cycle can form
  regardless of arrival order. No lock-ordering discipline is needed
  because there is no ordering to get wrong, and no deadlock regression
  test was added — §11 explicitly warns against manufacturing complexity
  to justify one.
- **Retries: none, and none needed.** Serialization failures (SQLSTATE
  40001) cannot occur under READ COMMITTED. Deadlocks (40P01) cannot occur
  with single-row locking. The two transient classes a retry would address
  are both unreachable by construction, so a retry loop would add a
  failure mode without removing one. Connection-level transience is
  `pgxpool`'s job and is handled there.

## Measured results

Against PostgreSQL 17 in Docker over loopback (Apple M3 Pro). These are
deployment properties — read them as ratios and regression signals, never
as advertised latencies.

| Scenario | Result |
|---|---|
| Same-key, 32 writers × 100, 3 rounds | 3200/3200 each round; **0 lost**; ~2,300 obs/s |
| First-write, 96 writers × 5 rounds | 96/96 each round; exactly 1 row |
| Multi-key, 32 keys × 100 | 3200/3200; ~5,000 obs/s |
| Mixed commit/cancel, 32 writers | 800 acknowledged == 800 stored |
| Cancelled lock wait | returned in **5.4 ms** |
| Exhausted pool, 2 s deadline | gave up at **2.0001 s** |
| Maximal baseline | 120 fingerprints, 64 delegators, **13,419 bytes** |
| 200 actors × 10 observations | 200 rows, exactly 2 tables |

**Multi-key is ~2.1× same-key throughput**, which is the evidence that
nothing serializes globally: if a table lock or advisory lock sat on the
write path, both figures would match.

## Test tiers

Three tiers, using standard toolchain flags rather than a second gating
mechanism:

```bash
go test ./...                                          # unit only — no database
TRUSTVIAN_TEST_POSTGRES_DSN=... go test -short ./...   # + integration
TRUSTVIAN_TEST_POSTGRES_DSN=... go test ./...          # + stress (release gate)
```

`TRUSTVIAN_TEST_POSTGRES_RESTART_CMD` additionally enables the
database-restart test. It is a separate variable because restarting a
server is destructive to whatever the DSN points at, and it should be run
on its own — a restart disrupts every other connection, and `go test ./...`
runs packages in parallel.

**`go test ./...` must never require PostgreSQL**, and does not.

## Migration upgrade path

`SchemaVersion` is 1. There is no v1→v2 upgrade because there has been no
second version, and inventing one to exercise the machinery would test a
fiction. What *is* provable today, and what actually runs in production on
every single process start, is re-migration against a populated database —
tested repeatedly, with full baseline equivalence asserted after each
round. Multi-version evolution belongs to whichever slice first changes
the layout.

## Corrupt state: the deliberate asymmetry

`Observe` returns `ErrCorruptState` and **does not overwrite** the
unreadable row. `Get` reports "no baseline", because the `Store` port
gives it no error return.

This is asymmetric on purpose. A backend that "recovered" from corruption
by writing a fresh baseline would silently erase an actor's entire learned
history and destroy the evidence needed to diagnose it. Refusing is
strictly better. `Get`'s reduced signal is fail-safe in the direction that
matters — the actor reads as unfamiliar, which *raises* anomaly rather
than suppressing it — and because `Get` never writes, the corrupt row
survives for `Observe` to surface on the next learning call.

Widening `Store.Get` to return an error was considered and rejected: it
would change the port for every implementation and caller to improve
reporting in a case that is already fail-safe and already loudly reported
on the write path. That is a port change looking for a justification, not
a defect being fixed.

## PostgreSQL version support

Tested against **PostgreSQL 17**. The features relied on are
`INSERT ... ON CONFLICT` (9.5+), `jsonb` (9.4+), transactional DDL (long
established), `pg_advisory_xact_lock` (9.1+), and `SELECT ... FOR UPDATE`.
Nothing requires a recent server. The supported statement is therefore
conservative: **PostgreSQL 13+ expected to work, 17 tested**, with any
narrower claim requiring a CI matrix that does not exist yet (task 037/038
territory).

## Acceptance Criteria

1. High-contention same-key `Observe` preserves every successful
   observation.
2. Concurrent first-write stress preserves every successful observation,
   in exactly one row.
3. Different-key operations remain isolated and do not serialize globally.
4. A failed transaction leaves the previous valid baseline intact.
5. Committed state survives a client restart.
6. Committed state survives a real PostgreSQL restart, with the restart
   itself verified.
7. Connection loss produces an explicit error; acknowledged writes equal
   stored state.
8. Startup against an unavailable database still fails closed.
9. Cancellation and deadline expiry surface via `errors.Is`.
10. A transaction waiting on a row lock is cancellable and leaks nothing.
11. Pool exhaustion is bounded and context-aware.
12. No connection or transaction leaks on any success or failure path.
13. Schema initialization is idempotent and safe under concurrent startup.
14. A newer, older, missing-with-data, or ambiguous schema version fails
    closed.
15. A failed migration leaves no partial state.
16. Re-migration preserves existing baseline data.
17. Corrupt persisted state fails explicitly and is never silently reset.
18. A maximal bounded baseline round-trips without truncation.
19. InMemory, FileStore, and PostgreSQL produce equivalent learned state
    and equivalent decisions.
20. Learning-eligibility (poisoning) and v0.7 agent semantics hold on
    every backend.
21. No raw-event warehouse; row count tracks distinct keys only.
22. No silent storage fallback and no credential leak introduced.
23. `go test ./...` passes with no PostgreSQL available.
24. No new production dependency.
