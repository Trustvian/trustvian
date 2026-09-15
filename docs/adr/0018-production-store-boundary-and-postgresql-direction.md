# 0018 — Production Store boundary and PostgreSQL direction

## Context

[docs/ROADMAP.md § v0.8](../ROADMAP.md#v08--production-runtime--storage)
opens with the objective "Trustvian OSS should be deployable as a real
production system, not only a library and CLI against a local file," and
names PostgreSQL as the preferred persistent `Store` candidate. Starting
that milestone surfaced a gap more basic than which database to add:

**No external consumer could select *any* `Store` implementation —
including the fully-implemented, durable `FileStore`.** Three facts
compose into that:

1. `store.Store` lives in `internal/store`, so an outside module cannot
   name the type.
2. Its methods reference `baseline.Baseline`,
   `fingerprint.Fingerprint`, and `features.VolatileFeatures` — all
   internal — so an outside module cannot *implement* the interface
   either. (Unlike a struct, receiving an interface by inference is not
   enough if you want to supply your own.)
3. No exported function anywhere returned a `store.Store` for
   pass-through, the way `config.CompilePolicy` returns a
   `policy.Policy` and `config.CompileAnomaly` returns an
   `anomaly.Config`.

So `trustvian.WithStore` was, in practice, in-module-only. Every
external deployment silently ran on `NewEngine`'s default
`store.NewInMemory()` and lost every learned baseline on restart. The
CLI did too: neither `trustvian analyze` nor `trustvian baseline build`
called `WithStore` at all, which made `baseline build` close to a dry
run — it learned from a corpus and discarded the result on exit.

This is the same class of gap `config.CompilePolicy` closed for Policy
in `v0.5` ([ADR 0008](0008-policy-config-boundary.md)) and
`config.CompileAnomaly` closed for anomaly scoring in `v0.7` ([ADR
0017](0017-public-anomaly-configuration-boundary.md)) — found, this
time, before adding the feature that would have inherited it.

## Decision 1: close the boundary before adding a backend

`config.StorageConfig` + `config.CompileStorage` become the public path
to store selection — a fourth independent config document alongside
`PolicyConfig`, `AlertConfig`, and `AnomalyConfig`, for the same reason
those three are independent of each other ([ADR
0009](0009-alert-config-is-a-separate-document.md)):

```
config.StorageConfig → CompileStorage → store.Store → Engine
```

`internal/store` is **not** promoted, and no internal type is aliased
publicly. `StorageConfig` is a discriminated union on `Type` rather than
a field-for-field mirror of one internal type, because `store.Store` is
an interface with several implementations taking different construction
parameters — this is the one config document in the package that
legitimately cannot mirror a single struct.

Ordering this before PostgreSQL was deliberate. Shipping a database
backend first would have produced a store only this module's own tests
could instantiate — satisfying the letter of "production-grade
persistence" while failing the milestone's actual objective.

### CompileStorage is not pure, unlike its siblings

`CompilePolicy`/`CompileAlerts`/`CompileAnomaly` are pure functions.
`CompileStorage` opens files (and, later, database connections). That
asymmetry is intentional: a caller wiring an `Engine` at startup wants a
load failure surfaced *then*, not on the first `Observe`. Callers who
want validity checked without touching the filesystem call
`cfg.Validate()`, which is pure and exposed for exactly that reason.

### Fail closed, never degrade

Any `CompileStorage` error returns a **nil** `Store`. A caller that
ignores the error cannot accidentally proceed on a silently substituted
in-memory store and lose the state it asked to persist. This matters
more than the equivalent guarantee for Policy or anomaly config:
misconfigured scoring produces different decisions, while a silently
substituted store produces *data loss*, which is invisible until the
restart that needed the data.

`StorageTypePostgres` is therefore **recognized but not implemented**:
it passes `Validate` (it is well-formed configuration) and fails
`CompileStorage` with a distinct `ErrStorageTypeNotImplemented` —
never `ErrInvalidStorageType`, so an operator can tell "I typo'd the
backend name" from "that backend is real but this release lacks it," and
never a fallback. Keeping validation and construction split this way
means the next slice changes `CompileStorage` only: no validation
change, and no config that already validated starts failing.

There is also no default `Type`. Omitting it is an error, not an
implicit choice of the non-durable backend — the same
fail-closed-on-ambiguous-config discipline `policy.Policy.Evaluate`
already applies to a missing default decision. A caller who wants
in-memory simply does not pass `WithStore`; `NewEngine`'s own default is
unchanged.

## Decision 2: the contract is written down and executable

"Production-grade" is meaningless against an unwritten contract, so the
`Store` contract now exists as a test suite (`TestStoreContract`,
`internal/store/contract_test.go`) run against every implementation from
one place, rather than as two parallel sets of near-identical
per-backend tests. The contract, as pinned:

| Guarantee | Meaning |
|---|---|
| Missing-key read | `Get` returns a zero-value-but-**keyed** `Baseline` and `false` — never nil, never an error. `internal/anomaly` scores a never-seen actor against exactly this value. |
| Read-after-write | `Get` after `Observe` reflects the observation. |
| `Observe` return value | The **post**-update state, identical to a following `Get`. |
| Incremental, not overwrite | `Observe` applies one observation to whatever exists; N calls accumulate. |
| Snapshot immutability | A `Baseline` previously returned by `Get` never changes when a later `Observe` lands. |
| Key isolation | One actor's observation never creates or mutates another's `Baseline`. |
| Same-key concurrency | N goroutines × M observations yields exactly N×M — **no lost updates**. |
| Distinct-key concurrency | Fully independent. |
| Sequence-state round-trip | `v0.6`/`v0.7` transition counters and the history window survive the port intact. |

Adding an implementation means adding one line to `storeFactories`. If
it cannot pass unmodified, either the implementation is wrong or the
contract changed — both worth forcing into the open.

The pre-existing per-implementation tests are kept, not replaced: they
cover genuinely backend-specific behavior the contract cannot
(`FileStore`'s restart durability, its atomic-rename write, its
deliberate non-persistence of freeze state).

## Decision 3: why the narrow port makes lost updates avoidable

This is the most consequential finding of the analysis, and it is good
news that could easily have gone the other way.

`Store.Observe` takes **the observation** — `(key, fp, vol, now)` — not
a caller-computed `Baseline`:

```go
Observe(ctx, key, fp, vol, now) (baseline.Baseline, error)
```

So the read-modify-write cycle lives entirely *inside* one
implementation call, where it can be wrapped in whatever lock or
transaction that backend needs. `InMemory` already does exactly this
under a per-key mutex. A PostgreSQL implementation can do the same with
a row lock:

```sql
BEGIN;
SELECT baseline FROM trustvian_baseline WHERE actor_id = $1 AND environment = $2 FOR UPDATE;
-- compute the new Baseline in Go via bl.Observe(fp, vol, now)
UPDATE trustvian_baseline SET baseline = $3, ... WHERE actor_id = $1 AND environment = $2;
COMMIT;
```

Had the port instead been `Save(ctx, key, bl)`, the read and the write
would span two separate calls with engine code in between, making lost
updates structurally unavoidable without adding optimistic versioning to
the domain model. [ADR
0004](0004-narrow-store-port-in-memory-only.md)'s decision to keep the
port narrow — "read the snapshot, apply one incremental update" — is
what makes a correct transactional backend possible at all, years
before one was written.

Note the separate `Get` in `Engine.Analyze` is **not** part of this
cycle and needs no transaction: `Analyze` reads to *score*, and
`Observe` later applies an increment rather than writing back what
`Analyze` read. A concurrent update between the two makes the scoring
read marginally stale; it never loses learning. Transaction scope
therefore stays inside `Observe` and must not be widened to span
`Analyze`, policy evaluation, or alert delivery.

## Decision 4: PostgreSQL remains the direction; FileStore stays

PostgreSQL is still the preferred next backend, for the reasons the
roadmap already recorded: transactional guarantees `FileStore`'s
whole-file rewrite cannot offer at write volume, broad operational
familiarity, real queryability for inspecting learned baselines, and an
OSS-friendly licensing and driver ecosystem. `FOR UPDATE` row locking
also makes the lost-update guarantee above straightforward rather than
clever.

**`FileStore` is not deprecated and remains a first-class option.** Its
actual limitations, stated without disparagement — it is a good fit for
single-process and local use:

- Every `Observe` serializes and rewrites the store's *entire* contents
  (measured: see [PERFORMANCE.md](../PERFORMANCE.md)); cost grows with
  total keys, not with the one key updated.
- Single-writer by design — two processes over one file would clobber
  each other, since the atomic rename protects readers from torn files,
  not writers from each other.
- Synchronous `fsync` per `Observe`.
- Inspection means reading a JSON blob; no ad hoc queries.
- Single-node durability only.

**PostgreSQL must never become a Core requirement.** `InMemory` and
`FileStore` remain fully supported, the default stays `InMemory`, and no
PostgreSQL dependency may appear in `go.mod` until code requires it —
none is added by this slice. `internal/anomaly`, `internal/policy`,
`internal/trust`, and `event` must never import a database driver; the
storage adapter depends inward on the `Store` contract, never the
reverse.

### Rejected: everything else

No Redis, Kafka, ClickHouse, OpenSearch, CDC, event sourcing, sharding,
leader election, or distributed consensus. A single PostgreSQL database
satisfies this milestone, and the roadmap's own non-goals reject
infrastructure added "because security products use it." Trustvian also
persists **current learned state**, not an event history: it is not a
SIEM or an event lake, so no raw-event warehouse and no retention
policy — `Baseline` is bounded current state, and nothing in this
milestone changes that.

## Decision 5: schema direction (not yet a schema)

Deferred to the implementation slice, but constrained now:

- **Smallest schema preserving current semantics.** `Baseline` is
  already a self-contained value keyed by `{ActorID, Environment}`. One
  row per key, with the baseline as a structured column, mirrors that
  directly. Normalizing `Fingerprints`/`PredecessorCounts`/
  `TrigramCounts` into separate tables is not justified by any known
  query need and would invite changing the domain model to suit SQL —
  which this ADR rules out.
- **Explicit version column.** `FileStore` already established that
  persistent representation is versioned (`fileSnapshotVersion`, with a
  loader that refuses unknown versions rather than misreading them). The
  SQL schema carries the same, plus a migration record. No heavyweight
  migration framework unless one proves necessary.
- **Domain model unchanged.** If SQL makes something awkward, the
  storage adapter absorbs the awkwardness.

## Decision 6: security and operational posture

- **Credentials never logged or wrapped into errors.** A DSN contains a
  password; `CompileStorage`'s file path is safe to echo, a DSN is not.
  The implementation slice must strip or omit credentials from every
  error it returns.
- **No SQL injection surface by construction**: parameterized queries
  only, and the only caller-supplied values are `ActorID`/`Environment`
  from a validated `Event` plus a serialized baseline.
- **Database unavailable → fail fast.** For `v0.8`, an explicitly
  configured store that cannot be reached fails startup. No degraded
  mode, no queue, no endless internal retry loop hiding a prolonged
  outage. Retry ownership sits with the deployment platform, not a
  hidden loop inside `Observe`.
- **Privacy unchanged.** Persisting introduces no new raw data: no
  prompt text, tool arguments, secret values, HTTP bodies, or raw SQL —
  `Baseline` holds none of these today, and the storage adapter persists
  `Baseline`, nothing more (see [ADR
  0014](0014-ai-agents-as-first-class-behavioral-actors.md)'s
  tool-argument privacy rule).
- **Least privilege.** The database role needs DML on its own tables and
  DDL only at migration time; documenting that split is the
  implementation slice's job.

### Integration testing

`go test ./...` must not require a running PostgreSQL. The
implementation slice gates database tests behind an explicit opt-in
(build tag or environment variable) and, since `v0.8` already plans a
Docker Compose reference deployment, can reuse that rather than adding a
container-orchestration test dependency. `TestStoreContract` is the
suite those integration tests run — not a parallel one.

## Consequences

- One new public config document (`StorageConfig`), one compiler
  (`CompileStorage`), two loaders (`LoadStorage`/`LoadStorageFile`), one
  CLI flag (`--storage-config`). No new dependency. No change to
  `store.Store`, `InMemory`, or `FileStore`.
- `WithStore` is usable by external consumers for the first time; the
  CLI can persist for the first time, which makes `baseline build`
  genuinely useful rather than a dry run.
- The `Store` contract is executable, so the next backend has a target
  instead of a blank page.
- `context.Context` is already on both `Store` methods, so no interface
  change is needed for cancellation — a database implementation honors
  it directly. The interface is untouched by this slice.
- PostgreSQL is unimplemented and fails closed when requested, rather
  than being absent and confusing.
