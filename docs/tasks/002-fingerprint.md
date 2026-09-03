# 002 — Behavioral Fingerprint: Documentation & Versioning

**Milestone:** v0.1 · **Depends on:** [001](001-feature-model.md) (if
`TargetCategory` is added, the fingerprint hash must incorporate it) ·
**Blocks:** none

## Objective

`internal/fingerprint.Compute` already satisfies every property the
roadmap asks for — deterministic, stable, explainable (retains
`Stable` alongside `ID`), cheap (136.7 ns/op, 12 allocs/op — see
[PERFORMANCE.md](../PERFORMANCE.md)), and proven under the exact hot
path that matters (`Engine.Analyze`, post-[ADR 0005](../adr/0005-fingerprint-computed-once-per-analyze.md)).
This task is not a rebuild. It closes two real gaps: a formal written
design doc (asked for explicitly in the roadmap brief), and a
composition-versioning story so that changing which stable dimensions
feed the hash (as 001 might) has a defined, non-silent migration
behavior.

## Why

Today, if the set of stable dimensions changes (e.g. 001 adds
`TargetCategory`), every existing `Fingerprint.ID` computed before the
change silently means something different after it — same ID string
space, different meaning, no way to tell old baselines from new ones
apart. For an in-memory-only `Store` (today's reality) this is
harmless — restart clears everything. Once 003 ships a persistent
`Store`, it stops being harmless: a stored baseline computed under one
fingerprint composition could be silently misinterpreted after a
version upgrade changes the hash inputs.

## Scope

- Write the fingerprint design doc: what goes into the hash, in what
  order, why FNV-1a (fast, non-cryptographic, sufficient — this is a
  content-addressed identity key, not a security boundary), and the
  field-boundary-collision protection already implemented (NUL
  separators between fields).
- Add a small, explicit version marker to the hash input (e.g. a
  version byte/string written first) so a future composition change
  produces a *disjoint* ID space from the current one, rather than a
  same-shaped-but-differently-meaning one. This is a one-line addition
  now, while it's cheap; retrofitting it after a persistent `Store`
  ships is the expensive version of this same change.
- If 001 lands first, incorporate `TargetCategory` into the hash as
  part of this same versioning change (one field-set change, one
  version bump, not two).

## Non-Goals

- No externally-visible "fingerprint algorithm registry" or
  pluggable-hash abstraction — one algorithm, versioned by a constant,
  matching [ADR 0001](../adr/0001-hexagonal-core-and-pipeline-shape.md)'s
  "add an interface only when a second implementation exists" stance.
- No change to `Fingerprint`'s public shape (`ID`, `Stable`) unless a
  version needs to be exposed for explainability — decide during
  implementation whether `Fingerprint.ID` alone (with the version
  baked in) is sufficient, or whether a separate `Version` field earns
  its keep for debugging/migration tooling.

## Technical Requirements

- Version marker written into the FNV-1a hash before the five (or six,
  post-001) stable fields, using the same NUL-separator convention
  already used between fields (see `writeField` in
  `internal/fingerprint/fingerprint.go`).
- Bumping the version must be a one-constant change with a comment
  explaining when to bump it (whenever the stable field set or hash
  algorithm changes).

## Tests

- Existing determinism/collision/volatile-independence tests
  (`internal/fingerprint/fingerprint_test.go`) continue to pass
  unmodified in shape (their assertions are about behavior, not exact
  ID values).
- New test: two `Compute` calls with the version constant changed
  between them (test-only technique, or a table entry once a second
  version genuinely exists) produce different IDs for otherwise
  identical input.

## Benchmarks

- Re-run `BenchmarkCompute` (`internal/fingerprint`) after adding the
  version marker; expect a negligible change (one more `writeField`
  call) — document the before/after numbers in
  [PERFORMANCE.md](../PERFORMANCE.md) rather than assuming "negligible."

## Documentation

- New content in [DOMAIN.md § Fingerprint](../DOMAIN.md#fingerprint):
  the versioning behavior.
- [ADR](../adr/): if the versioning mechanism involves a real
  trade-off decision (e.g. whether to expose `Version` as a field),
  record it — otherwise this is small enough to fold into 002's own
  notes without a dedicated ADR.

## Acceptance Criteria

- `go test ./internal/fingerprint/...` green.
- `BenchmarkCompute` numbers re-measured and documented, not assumed.
- Design doc content lands in `DOMAIN.md` (not a separate throwaway
  file) so it stays synchronized with the code long-term.
- If 001 shipped first, `TargetCategory` is part of the hash and
  covered by fingerprint tests.
