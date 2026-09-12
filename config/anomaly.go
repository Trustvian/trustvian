// Anomaly configuration is a separate document from PolicyConfig and
// AlertConfig, for the identical reason AlertConfig is
// (docs/adr/0009-alert-config-is-a-separate-document.md): Policy
// answers "what decision should Trustvian make," Alert answers "which
// Results should notify," and this document answers a third, distinct
// question — "how should Trustvian score behavioral deviation in the
// first place." All three are independently compiled
// (AnomalyConfig -> CompileAnomaly -> anomaly.Config) and this package
// keeps that independence at the configuration-document level too. See
// docs/adr/0017-public-anomaly-configuration-boundary.md for why this
// document exists at all: before it, an external caller had no way to
// construct an internal/anomaly.Config value — WithAnomalyConfig's
// parameter type lives under internal/, exactly the gap
// config.CompilePolicy already closed for policy.Policy in v0.5 (ADR
// 0008), now closed here for anomaly.Config.

package config

// AnomalySchemaVersionV1 is the only AnomalyConfig schema version this
// package currently accepts — independently versioned from
// SchemaVersionV1/AlertSchemaVersionV1 for the identical reason those
// two are independent of each other (see SchemaVersionV1's own doc
// comment and ADR 0008 § Schema versioning).
const AnomalySchemaVersionV1 = "v1"

// AnomalyConfig is the public, declarative description of the
// thresholds and weights internal/anomaly.Score combines signals
// with — compiled and validated by CompileAnomaly into the
// anomaly.Config value trustvian.WithAnomalyConfig accepts. It mirrors
// anomaly.Config's own field set exactly (every field there is
// operator-facing, documented, deliberately-defaulted configuration —
// there is no purely internal-implementation field to omit); it does
// not add or rename anything anomaly.Config itself does not already
// have.
//
// Zero-value handling follows two different rules, deliberately, not
// inconsistently — mirroring the identical distinction
// AlertConditionConfig.MinAnomalyScore/MaxTrustScore already
// establish:
//
//   - Fields whose canonical internal default is already 0
//     (every *Weight field below except NoveltyWeight/LatencyWeight/
//     ErrorWeight) are plain, non-pointer values: omitting one in
//     YAML/a zero-value struct literal means exactly what
//     anomaly.DefaultConfig() already means by "unset" — opt-in,
//     disabled, contributing nothing to Score. No ambiguity exists to
//     resolve.
//   - Fields whose canonical internal default is genuinely non-zero
//     (MinObservations=20, LatencyZThreshold/FrequencyZThreshold=3.0,
//     NoveltyWeight=1.0, LatencyWeight=0.6, ErrorWeight=0.8,
//     MinTransitionObservations/MinNGramObservations=20) are pointers:
//     nil means "use anomaly.DefaultConfig()'s own value", a set
//     pointer means "use this value instead". A plain float64/uint64
//     here would make omitting the field indistinguishable from
//     explicitly setting it to the Go zero value — for NoveltyWeight
//     specifically, that would silently disable categorical_novelty
//     for any caller who didn't happen to set every field, a real
//     "never silently weaken a security-relevant default" violation
//     (CLAUDE.md). This is exactly why CompileAnomaly(AnomalyConfig{})
//     — every pointer nil, every plain weight 0 — always produces
//     anomaly.DefaultConfig() byte-for-byte, proven by
//     TestCompileAnomalyZeroValueMatchesDefaultConfig.
type AnomalyConfig struct {
	// Version must be AnomalySchemaVersionV1 today. Required, not
	// defaulted — same reasoning as PolicyConfig.Version/
	// AlertConfig.Version.
	Version string `yaml:"version"`

	// MinObservations mirrors anomaly.Config.MinObservations: how many
	// times a Fingerprint must be observed to count as fully mature.
	// nil means anomaly.DefaultConfig()'s value (20).
	MinObservations *uint64 `yaml:"min_observations,omitempty"`

	// LatencyZThreshold mirrors anomaly.Config.LatencyZThreshold: the
	// |z-score| at which latency_deviation reaches full strength. Must
	// be a finite number > 0. nil means the default (3.0).
	LatencyZThreshold *float64 `yaml:"latency_z_threshold,omitempty"`

	// FrequencyZThreshold mirrors anomaly.Config.FrequencyZThreshold —
	// see FrequencyWeight's own doc comment below for why this and
	// FrequencyWeight must be calibrated together before enabling.
	// Must be a finite number > 0. nil means the default (3.0).
	FrequencyZThreshold *float64 `yaml:"frequency_z_threshold,omitempty"`

	// NoveltyWeight, LatencyWeight, and ErrorWeight mirror
	// anomaly.Config's own fields of the same name — each must be a
	// finite number in [0, 1]. nil means the default (1.0, 0.6, 0.8
	// respectively) — these three signals ship enabled by default, so
	// omitting them preserves that, not disables it.
	NoveltyWeight *float64 `yaml:"novelty_weight,omitempty"`
	LatencyWeight *float64 `yaml:"latency_weight,omitempty"`
	ErrorWeight   *float64 `yaml:"error_weight,omitempty"`

	// FrequencyWeight mirrors anomaly.Config.FrequencyWeight exactly,
	// including its own opt-in default (0) — see that field's own
	// extensive doc comment in internal/anomaly/anomaly.go for why no
	// single default is correct for every deployment. Must be a finite
	// number in [0, 1].
	FrequencyWeight float64 `yaml:"frequency_weight,omitempty"`

	// TimePatternWeight mirrors anomaly.Config.TimePatternWeight
	// (opt-in, default 0). Must be a finite number in [0, 1].
	TimePatternWeight float64 `yaml:"time_pattern_weight,omitempty"`

	// TransitionWeight mirrors anomaly.Config.TransitionWeight (`v0.6`
	// task 025, opt-in, default 0). Must be a finite number in [0, 1].
	TransitionWeight float64 `yaml:"transition_weight,omitempty"`

	// MinTransitionObservations mirrors
	// anomaly.Config.MinTransitionObservations. nil means the default
	// (20).
	MinTransitionObservations *uint64 `yaml:"min_transition_observations,omitempty"`

	// TransitionRarityWeight mirrors anomaly.Config.TransitionRarityWeight
	// (`v0.6` task 026, opt-in, default 0). Must be a finite number in
	// [0, 1].
	TransitionRarityWeight float64 `yaml:"transition_rarity_weight,omitempty"`

	// NGramWeight mirrors anomaly.Config.NGramWeight (`v0.6` task 027,
	// opt-in, default 0). Must be a finite number in [0, 1].
	NGramWeight float64 `yaml:"ngram_weight,omitempty"`

	// MinNGramObservations mirrors anomaly.Config.MinNGramObservations.
	// nil means the default (20).
	MinNGramObservations *uint64 `yaml:"min_ngram_observations,omitempty"`

	// NGramRarityWeight mirrors anomaly.Config.NGramRarityWeight
	// (`v0.6` task 027, opt-in, default 0). Must be a finite number in
	// [0, 1].
	NGramRarityWeight float64 `yaml:"ngram_rarity_weight,omitempty"`

	// MarkovWeight mirrors anomaly.Config.MarkovWeight (`v0.6` task
	// 028, opt-in, default 0) — see ADR 0013 for why enabling this
	// forces transition_rarity's own contribution to zero regardless of
	// TransitionRarityWeight's value; that mutual-exclusion rule lives
	// in internal/anomaly.Score itself and applies identically whether
	// Config was built via CompileAnomaly or a Go literal inside this
	// module. Must be a finite number in [0, 1].
	MarkovWeight float64 `yaml:"markov_weight,omitempty"`

	// DelegationWeight mirrors anomaly.Config.DelegationWeight (`v0.7`
	// task 031, opt-in, default 0). Must be a finite number in [0, 1].
	// Enabling this does not make Trustvian authenticate
	// Context.DelegatedFrom — see docs/SECURITY.md § AI Agent
	// behavioral security and ADR 0016.
	DelegationWeight float64 `yaml:"delegation_weight,omitempty"`

	// SensitiveTargetFloor mirrors anomaly.Config.SensitiveTargetFloor:
	// a Target name mapped to a minimum anomaly contribution that
	// always applies, regardless of learned familiarity. nil/empty
	// means no floors — the same "ships empty and inert" default
	// anomaly.DefaultConfig() itself uses. Every value must be a
	// finite number in [0, 1].
	SensitiveTargetFloor map[string]float64 `yaml:"sensitive_target_floor,omitempty"`
}
