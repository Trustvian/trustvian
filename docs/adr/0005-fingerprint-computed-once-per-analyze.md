# 0005 — Compute the Fingerprint once per `Analyze` call

## Context

`Engine.Analyze` needs a `fingerprint.Fingerprint` for two independent
reasons: to populate `Result.Fingerprint`, and to look up baseline
statistics inside `anomaly.Score`. The original `anomaly.Score`
signature (`Score(feat features.Features, bl baseline.Baseline, cfg
Config) Anomaly`) computed its own `fingerprint.Compute(feat.Stable)`
internally, so that callers wouldn't need to pass a redundant
parameter and the signature stayed close to the originally planned
shape.

This meant `Engine.Analyze` called `fingerprint.Compute` twice per
event — once directly (for `Result.Fingerprint`), once again inside
`anomaly.Score` — invisibly, since both call sites were "correct" in
isolation. It was only caught after adding a direct
`fingerprint.Compute` benchmark (134.8 ns/op, 12 allocs/op) during an
architecture-hardening pass and cross-referencing it against
`Engine.Analyze`'s allocation count: two fingerprint computations
accounted for roughly half of `Analyze`'s total allocations.

## Decision

`anomaly.Score` now takes the `fingerprint.Fingerprint` as an explicit
parameter (`Score(feat features.Features, fp fingerprint.Fingerprint,
bl baseline.Baseline, cfg Config) Anomaly`) instead of recomputing it.
`Engine.Analyze` computes it once and passes the same value to both
`Result.Fingerprint` and `anomaly.Score`.

## Alternatives considered

- **Leave `anomaly.Score` computing its own fingerprint**, treating
  the duplication as an acceptable, small, isolated cost. Rejected
  once measured: it was not small — roughly half of the hot path's
  allocations — and CLAUDE.md treats allocations on the hot path as a
  first-class concern, not a cosmetic one.
- **Cache the fingerprint inside `Features`** (compute it as part of
  `features.Extract` and carry it on the `Features` value). Rejected:
  `internal/features` sits before `internal/fingerprint` in the
  dependency direction (see
  [ARCHITECTURE.md](../ARCHITECTURE.md#dependency-direction)); giving
  `Features` a `Fingerprint` field would make `features` depend on
  `fingerprint`, inverting that direction for no benefit beyond saving
  one parameter at call sites.

## Consequences

- `anomaly.Score`'s public signature changed (a breaking change to
  this internal package's API, not the public SDK — `Engine.Analyze`'s
  own signature is unaffected, and this package is not importable from
  outside the module regardless).
- Measured effect: `Engine.Analyze` went from 563 ns/op, 552 B/op, 27
  allocs/op to 431 ns/op, 448 B/op, 15 allocs/op (-23% latency, -44%
  allocations). `anomaly.Score`'s common "familiar, nothing anomalous"
  path went from 175 ns/op, 12 allocs/op to 30 ns/op, 0 allocs/op. Full
  numbers: [PERFORMANCE.md](../PERFORMANCE.md).
- Any future caller of `anomaly.Score` (there is currently exactly
  one, `Engine.Analyze`) must already have computed the matching
  `Fingerprint` — a reasonable requirement, since every real caller
  needs one anyway.
