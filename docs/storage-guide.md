# Storage Guide

Trustvian learns. What it learns — each actor's `Baseline` — lives in a
`Store`. Which `Store` you choose decides whether that learning survives
a restart, and whether several Trustvian instances agree about what
"normal" means.

Three backends ship: **memory** (the default), **file**, and
**PostgreSQL** (since [task
035](tasks/035-postgresql-store-implementation.md)). All three are
selected through one public configuration document,
`config.StorageConfig`, compiled by `config.CompileStorage` — the same
pattern [`policy-guide.md`](policy-guide.md) and
[`anomaly-config-guide.md`](anomaly-config-guide.md) describe for Policy
and anomaly configuration ([ADR
0018](adr/0018-production-store-boundary-and-postgresql-direction.md)).

## Choosing a backend

| | `memory` | `file` | `postgres` |
|---|---|---|---|
| Survives restart | No | Yes | Yes |
| Shared between processes | No | No | **Yes** |
| Inspectable with external tools | No | JSON file | **SQL** |
| Setup required | None | A writable path | A database |
| Concurrent write cost | ~2 µs | ~4 ms (rewrites whole file) | ~0.6 ms (one row) |
| Default | **Yes** | No | No |

**Use `memory`** for tests, experiments, and any run whose learning you
do not need afterwards. It is the default precisely so that nothing
persists unless you asked for it.

**Use `file`** for a single-process deployment — a CLI run, a sidecar, one
long-lived service — where zero setup matters more than sharing. Note the
cost model: `FileStore` holds everything in memory and rewrites the entire
file on *every* `Observe`, so its write cost grows with total store size,
not with the one key that changed.

**Use `postgres`** when more than one process must agree. This is the case
that matters most and the one `file` cannot serve at all: two instances
with two files have two different baselines, so the same actor is
"familiar" to one and "novel" to the other, and the decision an event gets
depends on which replica answered it. That is not a performance problem,
it is an inconsistent security posture.

A useful surprise: PostgreSQL is not the slow option. Under concurrent
writes it is **roughly 6–17× faster than `file`**, because it updates one
row where `FileStore` rewrites everything. See
[`PERFORMANCE.md`](PERFORMANCE.md).

## Configuration

### YAML

```yaml
version: v1
type: memory
```

```yaml
version: v1
type: file
file:
  path: /var/lib/trustvian/baseline.json
```

```yaml
version: v1
type: postgres
postgres:
  dsn: postgres://trustvian:${PGPASSWORD}@db.internal:5432/trustvian?sslmode=require
  max_connections: 25          # optional; 0 = pgx default
  connect_timeout_seconds: 15  # optional; 0 = 10s
```

| Key | Type | Required | Default | Notes |
|---|---|---|---|---|
| `dsn` | string | **Yes** | — | PostgreSQL connection string. **A secret** — see [Credentials](#credentials). |
| `max_connections` | int32 | No | pgx default | Pool ceiling. Must not be negative. |
| `connect_timeout_seconds` | int | No | `10` | Bounds connect + ping + migration at startup. Must not be negative. |

Carrying an unused backend block is valid, so you can switch backends by
editing the `type:` line alone.

### CLI

```bash
trustvian analyze        --storage-config storage.yaml events.json
trustvian baseline build --storage-config storage.yaml corpus.json
```

Without `--storage-config`, the CLI uses the in-memory default and
`baseline build` is effectively a dry run.

### Go

```go
cfg, err := config.LoadStorageFile("storage.yaml")
if err != nil {
    return err
}
s, err := config.CompileStorage(cfg)
if err != nil {
    return err
}
if c, ok := s.(io.Closer); ok {
    defer c.Close()
}
engine := trustvian.NewEngine(trustvian.WithStore(s))
```

`config.CompileStorage` is the **only** way code outside this module can
obtain a `store.Store`: the interface lives in `internal/store` and its
methods reference internal types, so an external module can neither name
nor implement it. Passing the result straight into
`trustvian.WithStore` works by type inference without importing anything
internal — see [ADR
0008](adr/0008-policy-config-boundary.md).

## Lifecycle

`store.Store` has no `Close` method, deliberately: only database-backed
stores hold a releasable resource, and adding `Close` to the port would
force every implementation and every caller to change for a capability
most of them do not need. Releasing is instead an **optional,
type-asserted capability**, exactly like the existing `store.Freezer`:

```go
if c, ok := s.(io.Closer); ok {
    defer c.Close()
}
```

The assertion is a no-op for `memory` and `file`, so that snippet is
correct for every backend — write it once and stop thinking about it. The
PostgreSQL store satisfies `io.Closer` and closes its pool. A long-lived
process that never closes leaks connections at shutdown.

The PostgreSQL store deliberately does **not** implement
`store.Freezer`: freezing is a per-process concept, and a freeze that
silently applied to only one replica of a shared store would be
misleading about what it guaranteed.

## Failure behavior — it fails closed

A storage backend that cannot be built is an **error**, never a
substitution:

- `CompileStorage` returns a **nil `Store` and an error** on every
  failure path — an unreachable database, a refused authentication, a
  mismatched schema version, a missing DSN, an unreadable state file.
- There is **no fallback** from PostgreSQL to `file` or `memory`. Ever.
- The CLI aborts the command rather than analyzing anything.

This is not defensive coding for its own sake. A silent downgrade to
in-memory storage would mean every decision afterwards was made against
state the operator believed was durable and shared, and none of it would
survive the process — data loss disguised as convenience, discovered long
after it mattered. See [`SECURITY.md`](SECURITY.md).

What is *not* an error: a transient connection drop during normal
operation. `pgxpool` reconnects as ordinary pool behavior. Trustvian adds
no retry layer of its own, no reconnect loop, and no degraded mode — a
failed `Observe` returns an error to its caller, who decides.

## Concurrency semantics

`Observe` for a given key is **atomic and lost-update-free**, including
the very first observation for a key that does not exist yet.

Inside one transaction, `Observe`:

1. `INSERT ... ON CONFLICT DO NOTHING` — materializes the row.
2. `SELECT ... FOR UPDATE` — takes the row lock.
3. Decodes the baseline, applies `Baseline.Observe`, writes it back.

Step 1 is the non-obvious one. `SELECT ... FOR UPDATE` locks *nothing*
when no row matches, so without it two concurrent first observations would
each read empty and one would overwrite the other. Both protections are
verified by mutation testing: removing the lock loses ~160 of 200
concurrent observations, and removing the insert fails the
first-observation race test.

Distinct keys are distinct rows and never contend — different actors do
not serialize against each other.

Transactions are confined to `Observe`. Nothing else is ever inside one:
not `Analyze`, not policy evaluation, not alert delivery.

## Credentials

**The DSN is a secret.** It normally contains a password.

What Trustvian guarantees:

- The DSN is **never logged**.
- The DSN is **never wrapped into an error**. In particular,
  `pgxpool.ParseConfig`'s error is deliberately *not* wrapped: pgx
  redacts passwords in parseable URLs and in connection errors, but
  echoes an **unparseable** DSN verbatim. `ErrInvalidDSN` therefore
  reports that the DSN could not be parsed and withholds the detail.
  `TestNewStoreUnparseableDSNDoesNotLeakCredentials` is the regression
  test.
- **No credential is ever written to a row.** Nothing from the storage
  config is serialized into stored state; the `baseline` column holds a
  `baseline.Baseline` and nothing else.

What you are responsible for: keeping the DSN out of version control and
out of process listings. Supply it through a secret manager or an
environment variable your config loader expands, not a committed file.

## The schema

Two tables, both created automatically on first connection:

```
trustvian_baseline          one row per {actor_id, environment}
  actor_id          text        NOT NULL
  environment       text        NOT NULL
  baseline          jsonb       NOT NULL   -- authoritative state
  schema_version    integer     NOT NULL   -- derived
  fingerprint_count integer     NOT NULL   -- derived
  observation_count bigint      NOT NULL   -- derived
  last_observed     timestamptz            -- derived
  updated_at        timestamptz NOT NULL   -- derived
  PRIMARY KEY (actor_id, environment)

trustvian_schema_version   exactly one row: the version this DB is at
```

`baseline` is the single source of truth, and it uses the **identical**
`encoding/json` representation `FileStore` already writes — which is what
makes the two backends structurally equivalent rather than equivalent by
careful hand-matching, and means a future `Baseline` field persists in
both automatically.

Every other column is **derived from that jsonb and exists only for
inspection**. They are recomputed in the same statement that writes the
jsonb, so they cannot drift, and are never read back into a `Baseline`.

### Inspecting state

```sql
-- Which actors does Trustvian know about, and how much has it learned?
SELECT actor_id, environment, fingerprint_count, observation_count, last_observed
  FROM trustvian_baseline
 ORDER BY last_observed DESC NULLS LAST
 LIMIT 20;

-- Baselines that have gone quiet
SELECT actor_id FROM trustvian_baseline
 WHERE last_observed < now() - interval '7 days';
```

This inspectability is a reason to choose PostgreSQL, not a side effect:
no Trustvian-specific tool is needed to answer operational questions about
a live system.

### Migration and versioning

`Migrate` runs automatically at store construction and is:

- **idempotent** — a no-op after the first run;
- **transactional** — DDL and the version row commit together;
- **safe under concurrent startup** — a transaction-scoped advisory lock
  serializes racing processes;
- **fail-closed** — a recorded version that differs from this build's
  `SchemaVersion` aborts with `ErrSchemaVersionMismatch` rather than
  being silently upgraded or misread.

Automatic creation is chosen for the smallest safe OSS experience: one
connection string and it works. The cost is that the runtime role needs
table-creation rights on first run. To separate privileges, run one
startup with a migrating role, then switch the DSN to a role with only
`SELECT`/`INSERT`/`UPDATE` on the two tables — subsequent startups only
read the version row.

Evolution across multiple schema versions is [task
036](ROADMAP.md#v08--production-runtime--storage)'s scope, not this one's.

### What is *not* stored

No raw events. No `events`, `alerts`, `decisions`, `audit_log`,
`sessions`, or `agent_history` tables. Trustvian stores **bounded
behavioral state**, not history — `PredecessorCounts`, `TrigramCounts`,
and `DelegatorCounts` are all capped by `internal/baseline`, so a hostile
actor cannot grow a row without limit. Storing raw event history would
create a new, far more sensitive data asset than the one Trustvian needs.

## Running the integration tests

Tests that need a real server are gated on `TRUSTVIAN_TEST_POSTGRES_DSN`.
Unset, they skip — so `go test ./...` passes on a machine with no
PostgreSQL, which is a hard requirement, not a convenience.

```bash
docker run -d --name trustvian-pg -p 5433:5432 \
  -e POSTGRES_USER=trustvian \
  -e POSTGRES_PASSWORD=trustvian \
  -e POSTGRES_DB=trustvian_test \
  postgres:17

export TRUSTVIAN_TEST_POSTGRES_DSN='postgres://trustvian:trustvian@localhost:5433/trustvian_test?sslmode=disable'

go test -race ./...
(cd examples && go test ./...)

docker rm -f trustvian-pg
```

Each integration test creates a **private PostgreSQL schema** and drops it
afterwards, so tests never disturb each other — `go test ./...` runs
separate packages' binaries concurrently, and two packages here use
PostgreSQL. It also means these tests are safe to point at a database that
already holds data.

A reference Docker Compose environment is [task
037](ROADMAP.md#v08--production-runtime--storage)'s deliverable; the
command above is the minimum for running the tests today.

## Related reading

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — where `Store` sits in the
  pipeline and why the port is narrow
- [`SECURITY.md`](SECURITY.md) — fail-closed guarantees, credential
  handling, bounded state
- [`PERFORMANCE.md`](PERFORMANCE.md) — measured numbers for all three
  backends
- [ADR 0004](adr/0004-narrow-store-port-in-memory-only.md) — why
  `Observe` takes an observation, not a `Baseline`
- [ADR 0006](adr/0006-file-backed-persistent-store.md) — `FileStore`
- [ADR 0018](adr/0018-production-store-boundary-and-postgresql-direction.md)
  — the public selection boundary and the PostgreSQL direction
- [`examples/persistent-baseline`](../examples/persistent-baseline/) — a
  runnable external-consumer example
