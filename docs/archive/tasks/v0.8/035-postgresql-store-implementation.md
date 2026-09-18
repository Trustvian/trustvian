# 035 — PostgreSQL Store Implementation

**Milestone:** v0.8 — Production Runtime & Storage · **Depends on:**
[034](034-production-store-contract-and-public-boundary.md) (the
executable `Store` contract and the public `config.StorageConfig` /
`CompileStorage` selection boundary), [ADR
0004](../../../adr/0004-narrow-store-port-in-memory-only.md) (the narrow port
whose `Observe` shape makes a transactional backend possible), [ADR
0018](../../../adr/0018-production-store-boundary-and-postgresql-direction.md)
· **Blocks:** 036 (durability/concurrency/migration hardening), 037
(reference Docker Compose deployment) · **Second slice of `v0.8`.**

## Objective

Make `type: postgres` functional: a production-grade, concurrency-correct
PostgreSQL implementation of the existing `internal/store.Store` port,
selectable through the public boundary 034 built, with no change to the
port, the domain model, or the Engine.

Task 034 ended with PostgreSQL *recognized but unimplemented* —
`CompileStorage` returned `ErrStorageTypeNotImplemented`. This slice
removes that state.

## Why

`FileStore` is durable but single-process: it holds the whole store in
memory and rewrites the entire file on every `Observe`. That makes it
fine for a CLI or a single sidecar and unusable for the deployment shape
`v0.8` exists to support — several Trustvian instances behind a load
balancer, all of which must agree on what an actor's normal behavior is.
Two instances with two files have two different baselines, so the same
actor is "familiar" to one and "novel" to the other, and the decision an
event receives depends on which replica answered it. Shared state is not
an optimization here; it is what makes a horizontally-scaled deployment's
decisions coherent at all.

PostgreSQL also buys something `FileStore` structurally cannot:
inspectability. An operator can answer "which actors does Trustvian know
about, how much has it learned, when did it last update?" with SQL,
against the live system, without a Trustvian-specific tool.

## Scope

```text
internal/store/postgres/          (new package)
  postgres.go                       Store: Get, Observe, Close
  schema.go                         DDL, SchemaVersion, Migrate
config/storage.go                 + PostgresStorageConfig
config/validate.go                + ErrMissingStorageDSN
config/compile.go                 StorageTypePostgres case implemented
cmd/trustvian/policy.go           newEngine returns a cleanup func
internal/store/contract_test.go   + PostgreSQL factory (one line)
```

- **A separate package**, `internal/store/postgres`, not new files in
  `internal/store`. This is what keeps `pgx` out of the build of every
  package that imports `internal/store` — including `internal/baseline`
  consumers and the OTel processor.
- **One driver: `github.com/jackc/pgx/v5`** (with `pgxpool`). Native
  `context` support, native pooling, native transactions. The module's
  dependency count goes from 1 direct (OTel) + `yaml` to add exactly one
  more direct dependency.
- **One row per `baseline.Key`**, primary key `(actor_id, environment)`.
- **Authoritative state in one `jsonb` column**, marshalled with the
  same `encoding/json` representation `FileStore` already writes. The
  scalar columns beside it (`schema_version`, `fingerprint_count`,
  `observation_count`, `last_observed`, `updated_at`) are *derived and
  inspection-only*, recomputed in the same statement that writes the
  jsonb so they cannot drift, and never read back into a `Baseline`.
- **`Observe` is one transaction**: materialize the row, lock it, read,
  apply `Baseline.Observe`, write. Nothing outside `Observe` is ever
  inside a transaction — not `Analyze`, not policy evaluation, not alert
  delivery.
- **`Migrate` is idempotent, transactional, and concurrent-startup-safe**
  via a transaction-scoped advisory lock; a recorded version that differs
  from `SchemaVersion` aborts rather than being upgraded.
- **`Close()` via `io.Closer`**, a type-asserted optional capability
  exactly like the existing `store.Freezer` — not a new method on
  `store.Store`.

## Non-Goals

- **No change to `store.Store`.** The port's `Get`/`Observe` shape was
  sufficient; ADR 0018 predicted this and it held.
- **No PostgreSQL concepts in the core.** `event` through
  `internal/policy` and the root `Engine` remain unaware this package
  exists. Nothing but `config/compile.go` imports it.
- **No new tables beyond the two.** No raw event table, no `events`,
  `alerts`, `decisions`, `audit_log`, `sessions`, or `agent_history`.
  Trustvian stores bounded behavioral state, not history — this is a
  security property (see [ADR
  0018](../../../adr/0018-production-store-boundary-and-postgresql-direction.md)),
  not a storage preference.
- **No normalization of `Baseline`'s internal maps** into per-fingerprint
  or per-transition tables. See `schema.go`'s rationale: it would reshape
  the domain model to suit SQL, turn one atomic row update into a
  multi-table write, and invite exactly the unbounded-history table
  above.
- **No retry loop, no reconnect loop, no degraded mode.** `pgxpool`
  reconnects within a pool as a normal pool behavior; Trustvian adds no
  layer above it, and never falls back to another backend.
- **`FileStore` is not deprecated; InMemory remains the default.** An
  operator who changes nothing sees byte-for-byte the previous behavior.
- **No testcontainers.** Integration tests are DSN-gated. Task 037 owns
  the canonical container environment.
- **Not 036.** Correctness is here; 036 is hardening (backup/restore
  guidance, multi-version migration, failure-injection matrix).

## Concurrency correctness

The lost-update problem has two halves, and the second is the one that is
easy to miss.

**Existing row.** `SELECT ... FOR UPDATE` inside the transaction
serializes concurrent `Observe` calls for that key. Different keys are
different rows and never contend.

**Absent row.** `SELECT ... FOR UPDATE` locks *nothing* when no row
matches, so two concurrent *first* observations for a new key would each
read empty, each compute a baseline with one observation, and one would
overwrite the other. `Observe` therefore runs `INSERT ... ON CONFLICT
(actor_id, environment) DO NOTHING` **before** taking the lock, so the
row provably exists by the time `FOR UPDATE` runs and the lock always has
something to hold.

Both halves are proven by mutation testing, not asserted: removing
`FOR UPDATE` loses ~160 of 200 concurrent same-key observations, and
removing the pre-insert fails the first-observation race test. Neither
protection is decorative.

## Security

| Property | How |
|---|---|
| No credential leak | The DSN is never logged and never wrapped into an error. `pgxpool.ParseConfig` errors specifically are **not** wrapped — pgx redacts passwords in parseable URLs and in connection errors, but echoes an *unparseable* DSN verbatim. `TestNewStoreUnparseableDSNDoesNotLeakCredentials` is the regression. |
| No credentials in state | Nothing from `Config` is serialized into a row. The `baseline` column holds `baseline.Baseline` and nothing else. |
| No SQL injection | Every value is a bound parameter. The only Go-assembled parts of any statement are the two table-name constants in `schema.go`, which are compile-time literals. Actor IDs, environments, fingerprint IDs, target names, and operations are all parameters. |
| Fail closed | An unreachable, unauthenticated, misconfigured, or schema-incompatible database is an error from `CompileStorage` with a nil Store. There is no code path from "PostgreSQL is unavailable" to a working in-memory or file store. Silent persistence downgrade is prohibited. |
| Bounded state | `PredecessorCounts`, `TrigramCounts`, and `DelegatorCounts` stay bounded by `baseline`'s own caps; PostgreSQL stores the same bounded value, so a hostile actor cannot grow a row without limit. |

## Tests

- **`internal/store/contract_test.go`** — the nine contract guarantees
  now run against PostgreSQL by adding one factory. **No contract test
  was modified to make PostgreSQL pass.** It passes them as written,
  including `ConcurrentObserveSameKeyLosesNoUpdates` (200 concurrent
  observations, zero lost) and
  `TestStoreContractImplementationsAgreeOnLogicalState`.
- **`internal/store/postgres/postgres_test.go`** — three tests that need
  no database (empty DSN, unparseable DSN credential redaction,
  unreachable database) and eight DSN-gated ones (first-observation race,
  restart durability across separate pools, `Observe`/`Get` cancellation,
  inspection columns, migration idempotency under concurrent startup,
  schema-version mismatch, per-key non-serialization).
- **`cmd/trustvian/main_test.go`** — `baseline build` then a separate
  `analyze`, both through `--storage-config` against PostgreSQL; plus
  fail-closed-on-unreachable with a no-password-leak assertion.
- **`examples/persistent-baseline/main_test.go`** — the external-consumer
  proof, from a module that cannot import `internal/*`: public config →
  `CompileStorage` → `WithStore`, learn, close the pool, rebuild, and
  find the baseline still there.
- **Test isolation.** Integration tests create a **private PostgreSQL
  schema** per store and drop it afterwards. An earlier version truncated
  a shared table instead; that was wrong, because `go test ./...` runs
  separate packages' binaries *concurrently* and two packages here use
  PostgreSQL — each truncation wiped rows the other was mid-way through
  counting, producing phantom lost-update failures against a correct
  implementation. Schema-per-store removes the shared resource rather
  than trying to time-share it.
- **`go test ./...` must pass with no PostgreSQL available**, and does:
  every test needing a server skips on an unset
  `TRUSTVIAN_TEST_POSTGRES_DSN`.

## Performance

`internal/store/postgres/postgres_bench_test.go`, split the same way as
the existing store benchmarks. Measured against PostgreSQL 17 in Docker
on loopback (Apple M3 Pro) — these are deployment properties, useful as
ratios and regression signals, not as advertised latencies:

| Benchmark | ns/op |
|---|---|
| `BenchmarkGet` | ~152,000 |
| `BenchmarkObserveDistinctKeys` | ~238,000 |
| `BenchmarkObserveSameKey` | ~600,000 |

Same-key is ~2.5× distinct-keys, which is the row lock doing its job.

The comparison worth recording: `BenchmarkFileStoreObserveSameKey` is
~3,960,000 ns/op, so **PostgreSQL is roughly 6–17× faster than
`FileStore` under concurrent write load** — because `FileStore` rewrites
its entire contents on every `Observe` while PostgreSQL updates one row.
The durable backend that scales is also the faster one here; `FileStore`
remains the right choice only for single-process use where its zero setup
cost matters.

## Documentation

- [`docs/storage-guide.md`](../../../storage-guide.md) (new) — the three
  backends, when to use each, configuration, security, failure and
  concurrency semantics, schema inspection, lifecycle, and how to run the
  integration tests.
- `docs/ARCHITECTURE.md`, `docs/SECURITY.md`, `docs/PERFORMANCE.md`,
  `docs/ROADMAP.md`, `README.md`, `CHANGELOG.md` updated.
- No new ADR: ADR 0018 already recorded the decision to implement
  PostgreSQL and why the port supports it. This slice executed that
  decision without departing from it.

## Acceptance Criteria

1. `internal/store/postgres` implements `store.Store` and passes all nine
   contract guarantees against a real PostgreSQL server, **unmodified**.
2. `config.CompileStorage` builds a working PostgreSQL store from a YAML
   document, through public API only.
3. Concurrent `Observe` for the same key loses no updates — including the
   first observation for a key that does not exist yet.
4. State written by one process is read by a separate one.
5. An unreachable or misconfigured database fails closed: error, nil
   Store, no fallback to any other backend.
6. No DSN, password, or credential appears in any log, error, or
   persisted row.
7. Every SQL value is a bound parameter; no caller-supplied string is
   concatenated into a statement.
8. Schema creation is idempotent and safe under concurrent startup; a
   mismatched recorded version aborts.
9. `go test ./...` passes with no PostgreSQL installed.
10. `gofmt -l`, `go vet ./...`, `go test ./...`, `go test -race ./...`
    clean in all three modules; InMemory remains the default and
    `FileStore` is unchanged.
