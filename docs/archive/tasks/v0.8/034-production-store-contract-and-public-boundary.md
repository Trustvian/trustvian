# 034 — Production Store Contract & Public Selection Boundary

**Milestone:** v0.8 — Production Runtime & Storage · **Depends on:**
`v0.1` ([ADR 0004](../../../adr/0004-narrow-store-port-in-memory-only.md)'s
narrow `Store` port, [ADR
0006](../../../adr/0006-file-backed-persistent-store.md)'s `FileStore`);
`v0.5`/`v0.7`'s config-boundary precedent ([ADR
0008](../../../adr/0008-policy-config-boundary.md), [ADR
0017](../../../adr/0017-public-anomaly-configuration-boundary.md)) ·
**Blocks:** 035 (PostgreSQL Store) — which cannot be written correctly
without the contract and boundary this task establishes ·
**First slice of `v0.8`.**

## Objective

Establish the production-persistence *contract* and the public
*selection boundary* before adding a database backend, and answer the
question `v0.8` actually turns on:

> What guarantees must a production Store provide that FileStore does
> not — and can an operator select one at all?

## Why

Scoping `v0.8` surfaced a gap more basic than choosing a database:
**no external consumer could select any `Store` implementation, not even
the durable `FileStore` that already ships.** `store.Store` is in
`internal/store`; its methods reference internal types, so an outside
module can neither name nor implement it; and no exported function
returned one for pass-through. `trustvian.WithStore` was in-module-only
in practice, so every external deployment silently ran on the default
in-memory store and lost every baseline on restart.

Trustvian's own CLI was in the same position — neither `analyze` nor
`baseline build` called `WithStore`, making `baseline build` close to a
dry run: it learned from a corpus and threw the result away on exit.

Shipping PostgreSQL first would have inherited all of this: a
production-grade backend only this module's own tests could instantiate,
satisfying the letter of the milestone while missing its objective
("deployable as a real production system, not only a library and CLI
against a local file"). So this slice fixes reachability and writes down
the contract; 035 implements the backend against it.

## Scope

```text
config.StorageConfig            (new public document)
        ↓
config.CompileStorage           (new; validates, opens, fails closed)
        ↓
store.Store                     (unchanged internal port)
        ↓
trustvian.WithStore → Engine    (unchanged signature)

internal/store/contract_test.go (new; one contract, every implementation)
cmd/trustvian --storage-config  (new flag on analyze + baseline build)
```

- `config/storage.go` (new): `StorageConfig` (`Version`, `Type`,
  `File`), `FileStorageConfig`, `StorageSchemaVersionV1`, and the three
  `StorageType*` constants. A discriminated union on `Type`, not a
  field-for-field mirror — `store.Store` is an interface with
  implementations taking different parameters, unlike the single structs
  the other three config documents mirror.
- `config/validate.go`: `StorageConfig.Validate` plus four sentinels
  (`ErrUnsupportedStorageVersion`, `ErrInvalidStorageType`,
  `ErrMissingStoragePath`, `ErrStorageTypeNotImplemented`).
- `config/compile.go`: `CompileStorage`.
- `config/load.go`: `LoadStorage`/`LoadStorageFile`.
- `internal/store/contract_test.go` (new): `TestStoreContract` — nine
  contract guarantees × every implementation — plus
  `TestStoreContractImplementationsAgreeOnLogicalState`.
- `cmd/trustvian`: `--storage-config <path>` on both subcommands,
  threaded through the existing `newEngine` helper.
- `examples/persistent-baseline/` (new): the external-consumer proof.
- **No PostgreSQL implementation. No new dependency.**

## Non-Goals

- **No PostgreSQL implementation** — that is 035. `StorageTypePostgres`
  is recognized and fails closed with
  `ErrStorageTypeNotImplemented`, so an operator who read the roadmap
  and tried it gets a precise answer rather than "unknown type" or, far
  worse, a silent downgrade.
- **No Docker/Compose deployment** — a later slice.
- **No `store.Store` interface change.** `context.Context` is already on
  both methods, so nothing is needed for cancellation.
- **No promotion of `internal/store`**, no public alias of an internal
  type.
- **No Redis/Kafka/ClickHouse/OpenSearch/CDC/event-sourcing/sharding/
  leader-election**, no raw-event warehouse, no retention policy.
- **No schema** — direction only, constrained in ADR 0018.
- **No strategic post-`v0.7` capability** pulled in: no MCP/tool
  security, runtime provenance, numeric behavioral baselines, or
  Control-plane work.

## The Store contract, as pinned

Documented by `TestStoreContract`, executable against every
implementation:

| Guarantee | Meaning |
|---|---|
| Missing-key read | `Get` → zero-value-but-keyed `Baseline`, `false`. Never nil, never an error. |
| Read-after-write | `Get` after `Observe` reflects it. |
| `Observe` return value | Post-update state, identical to a following `Get`. |
| Incremental, not overwrite | N `Observe` calls accumulate to N observations. |
| Snapshot immutability | A `Baseline` already returned by `Get` never mutates under a later `Observe`. |
| Key isolation | One actor's observation never touches another's `Baseline`. |
| Same-key concurrency | N goroutines × M observations = exactly N×M. **No lost updates.** |
| Distinct-key concurrency | Independent. |
| Sequence-state round-trip | `v0.6`/`v0.7` transition counters and history window survive intact. |

## Production-store requirements (for 035)

| Requirement | Status after this slice |
|---|---|
| Durability | Contract-adjacent; `FileStore` flushes per `Observe`. 035 must define acknowledged-write durability for SQL. |
| Atomicity | **Required and testable now** — same-key concurrency contract test. |
| Concurrency / lost updates | Analyzed; solvable via row lock because the port takes an *observation*, not a `Baseline`. See ADR 0018 § Decision 3. |
| Transaction scope | Constrained now: inside `Observe` only. Never spanning `Analyze`, policy evaluation, or alert delivery. |
| Schema versioning | Direction set (version column + migration record); no schema yet. |
| Cancellation | No interface change needed — `context.Context` already present. |
| Failure behavior | Decided: fail fast. No degraded mode, no hidden retry loop. |
| Credentials | Decided: never logged, never wrapped into errors. |

## Tests

`internal/store/contract_test.go` (new): `TestStoreContract` (9
guarantees × `InMemory`, `FileStore` = 18 subtests) and
`TestStoreContractImplementationsAgreeOnLogicalState`. The
**same-key-concurrency** subtest is the one a naive
read-then-compute-then-write backend fails, and the reason it exists
before the database does.

`config/storage_test.go` (new, 21 tests): validation (version, missing
and unknown `Type`, `file` without a path, `postgres` accepted at
validation time, unused backend block ignored); compilation (memory,
file, load-failure propagation, nil-Store-on-error); **`TestCompileStoragePostgresFailsClosedNeverFallsBack`**;
loaders (strict decoding, duplicate keys, empty input, file reads);
`FuzzLoadStorage`.

`cmd/trustvian/main_test.go` (+6): flag acceptance on both subcommands,
invalid/missing/unimplemented configs all failing closed, and
**`TestBaselineBuildThenAnalyzePersistsAcrossCommands`** — `baseline
build` writes learned state, a *separate* `analyze` invocation reads it
back, asserted on `categorical_novelty`'s own Detail string ("never
observed" → "observed N/20 times"). Before this slice the second run
could only ever report the former.

`examples/persistent-baseline/main_test.go` (new, 3 tests, run from the
**separate** `examples` module — the external-consumer proof):
`TestPublicConfigSelectsDurableStoreAcrossRestart`,
`TestPublicConfigMemoryStoreDoesNotPersist` (the control),
`TestPublicConfigUnimplementedBackendFailsClosed`.

## Example

`examples/persistent-baseline/` — learns across 12 events, discards the
`Engine`, rebuilds from the same file, and shows the baseline survived.
Verified with real captured `go run .` output in its README, and via
`make examples`.

## Performance

No hot-path change: `CompileStorage` runs once at construction, and
`Engine.Analyze`/`Observe` call the same `Store` methods as before.
`BenchmarkEngineAnalyze` and `internal/store`'s existing
`BenchmarkInMemory*`/`BenchmarkFileStore*` are the relevant baselines and
are unchanged. PostgreSQL benchmarking belongs to 035, which must also
keep database timing out of unit tests — see [PERFORMANCE.md § v0.8
persistence](../../../PERFORMANCE.md).

## Documentation

- [ADR 0018](../../../adr/0018-production-store-boundary-and-postgresql-direction.md)
  (new): the full design — boundary, contract, lost-update analysis,
  PostgreSQL direction, rejected infrastructure, schema/security
  posture.
- [CHANGELOG.md § v0.8](../../../../CHANGELOG.md#v080--production-runtime--storage):
  slice sequence (034–038), this task marked done.
- [ARCHITECTURE.md](../../../ARCHITECTURE.md): the storage boundary alongside
  the existing Policy/Anomaly config boundaries.
- [DOMAIN.md](../../../DOMAIN.md): what persists, and the contract.
- [SECURITY.md](../../../SECURITY.md): storage-config validation and
  production-persistence threats.
- [PERFORMANCE.md](../../../PERFORMANCE.md): measurement plan for 035.
- [cli-guide.md](../../../cli-guide.md): `--storage-config`.
- [README.md](../../../../README.md) / [CHANGELOG.md](../../../../CHANGELOG.md).

## Acceptance Criteria

- `go test ./... -race -count=1` green, including every new test.
- `TestStoreContract` passes for both existing implementations, and a
  new implementation needs only one line in `storeFactories`.
- `TestCompileStoragePostgresFailsClosedNeverFallsBack` and
  `TestRunAnalyzeUnimplementedStorageBackendFailsClosed` both pass —
  an unimplemented backend never yields a substitute store.
- `TestBaselineBuildThenAnalyzePersistsAcrossCommands` passes — the CLI
  can persist across invocations.
- `TestPublicConfigSelectsDurableStoreAcrossRestart` passes from the
  separate `examples` module, with no `internal/*` import.
- `InMemory`/`FileStore` behavior unchanged; `NewEngine`'s default
  unchanged; existing configs unaffected (storage is a separate,
  optional document).
- No new dependency (`git diff -- go.mod go.sum processor/go.mod
  processor/go.sum` empty), no PostgreSQL code, no schema.
- 035's scope is unambiguous: implement `store.Store` over PostgreSQL,
  satisfy `TestStoreContract` unmodified, add the backend's own case to
  `CompileStorage`.
