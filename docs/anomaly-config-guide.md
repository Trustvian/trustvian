# Anomaly Configuration Guide

`internal/anomaly.Score` combines behavioral signals using thresholds
and weights held in `anomaly.Config`. Since [task
033](tasks/033-v07-stabilization-release-gate.md) ([ADR
0017](adr/0017-public-anomaly-configuration-boundary.md)), a caller
outside this module configures that value through `config.AnomalyConfig`
and `config.CompileAnomaly` — mirroring
[`docs/policy-guide.md`](policy-guide.md)'s own `PolicyConfig`/
`CompilePolicy` pattern exactly, as a third independent document
alongside Policy and Alert configuration (see [ADR
0009](adr/0009-alert-config-is-a-separate-document.md) for why these
stay separate).

## The field reference

| Field | YAML key | Type | Default | Valid range | Enabled by default? |
|---|---|---|---|---|---|
| `MinObservations` | `min_observations` | `*uint64` | `20` | any `uint64` | n/a (maturity threshold) |
| `LatencyZThreshold` | `latency_z_threshold` | `*float64` | `3.0` | finite, `> 0` | n/a (threshold) |
| `FrequencyZThreshold` | `frequency_z_threshold` | `*float64` | `3.0` | finite, `> 0` | n/a (threshold) |
| `NoveltyWeight` | `novelty_weight` | `*float64` | `1.0` | finite, `[0, 1]` | **Yes** |
| `LatencyWeight` | `latency_weight` | `*float64` | `0.6` | finite, `[0, 1]` | **Yes** |
| `ErrorWeight` | `error_weight` | `*float64` | `0.8` | finite, `[0, 1]` | **Yes** |
| `FrequencyWeight` | `frequency_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in |
| `TimePatternWeight` | `time_pattern_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in |
| `TransitionWeight` | `transition_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in (`v0.6`) |
| `MinTransitionObservations` | `min_transition_observations` | `*uint64` | `20` | any `uint64` | n/a (maturity threshold) |
| `TransitionRarityWeight` | `transition_rarity_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in (`v0.6`) |
| `NGramWeight` | `ngram_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in (`v0.6`) |
| `MinNGramObservations` | `min_ngram_observations` | `*uint64` | `20` | any `uint64` | n/a (maturity threshold) |
| `NGramRarityWeight` | `ngram_rarity_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in (`v0.6`) |
| `MarkovWeight` | `markov_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in (`v0.6`) |
| `DelegationWeight` | `delegation_weight` | `float64` | `0` | finite, `[0, 1]` | No — opt-in (`v0.7` task 031) |
| `SensitiveTargetFloor` | `sensitive_target_floor` | `map[string]float64` | `{}` | each value finite, `[0, 1]` | No — opt-in, empty by default |

**Security/performance implications**, briefly (each field's own
internal doc comment in `internal/anomaly/anomaly.go` carries the full
reasoning — this table does not duplicate it):

- Every opt-in weight is still **computed and reported** in
  `Anomaly.Contributors` regardless of its own value — only its
  *contribution to `Score`* is gated by the weight. Disabling a signal
  never hides it from an operator reading the explanation; it only
  keeps it from driving `Decision`.
- `MarkovWeight > 0` forces `TransitionRarityWeight`'s own contribution
  to zero, regardless of its configured value — the two are proven
  monotonic reparameterizations of the identical evidence (ADR 0013);
  enabling both is not double-counting, by construction.
- `SensitiveTargetFloor` is the one field that **cannot be learned
  away** — see `docs/SECURITY.md § Some risk cannot be learned away`.
- Raising `DelegationWeight` does not make Trustvian authenticate
  `Context.DelegatedFrom` — see ADR 0016 and `docs/SECURITY.md § AI
  Agent behavioral security`.

## Zero-value semantics — two rules, not one

Fields whose canonical default is already `0` (every opt-in `*Weight`
field) are plain `float64` values: omitting one means exactly what it
already means today — disabled. Fields whose canonical default is
non-zero (`MinObservations`, the two `Z-Threshold`s,
`NoveltyWeight`/`LatencyWeight`/`ErrorWeight`, the two
`Min*Observations` fields) are pointers: `nil` means "use the existing
default," a set pointer means "use this value instead." This is not
an accident of Go's zero-value rules — see [ADR
0017](adr/0017-public-anomaly-configuration-boundary.md) for why a
plain value here would risk silently disabling `categorical_novelty`
for any caller who didn't set every field.

**Configuring nothing preserves existing behavior, proven not
assumed**: `CompileAnomaly(AnomalyConfig{Version: "v1"})` reproduces
`anomaly.DefaultConfig()` byte-for-byte
(`TestCompileAnomalyZeroValueMatchesDefaultConfig`).

## Configuring from Go

```go
import (
	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
)

cfg := config.AnomalyConfig{
	Version:          config.AnomalySchemaVersionV1,
	DelegationWeight: 0.7,
	NGramWeight:      0.9,
}
ac, err := config.CompileAnomaly(cfg)
if err != nil {
	log.Fatal(err)
}

engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(ac))
```

`ac`'s static type is `anomaly.Config` — the same internal type
`internal/anomaly` itself uses — but this code never names that type or
imports `internal/anomaly`: `ac` is received from `CompileAnomaly` and
passed straight into `trustvian.WithAnomalyConfig` via Go's normal type
inference, the identical mechanism `config.CompilePolicy`'s own return
value already relies on (see [ADR
0008](adr/0008-policy-config-boundary.md)).

## Loading from a YAML file

```yaml
# anomaly.yaml
version: v1
delegation_weight: 0.7
ngram_weight: 0.9
sensitive_target_floor:
  secrets-manager: 0.8
```

```go
cfg, err := config.LoadAnomalyFile("anomaly.yaml")
if err != nil {
	log.Fatal(err)
}
ac, err := config.CompileAnomaly(cfg)
if err != nil {
	log.Fatal(err)
}
engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(ac))
```

Decoding is strict — an unrecognized field, or a duplicate mapping key,
fails loudly, identical to `PolicyConfig`/`AlertConfig`'s own loaders.
A negative literal into a `uint64` field (`min_observations`, etc.) is
rejected by the decoder itself, never silently wrapped into a huge
positive value.

## From the CLI

```bash
trustvian analyze --anomaly-config anomaly.yaml events.json
trustvian baseline build --anomaly-config anomaly.yaml corpus.json
```

Combine with `--config <path>` (Policy) freely — the two are
independently loaded and compiled, matching this doc's own "approval is
policy data, never an anomaly weight" principle.

## What stays internal

`internal/baseline`'s resource-exhaustion bounds
(`maxPredecessors`/`maxTrigramPredecessors`/`maxDelegators`, each `64`)
are **not** configurable, from any surface, deliberately — see ADR
0017's own reasoning for why raising them speculatively would be
exactly the kind of unbounded-state risk this project avoids.
