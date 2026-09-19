// Trust configuration is a separate document from PolicyConfig,
// AlertConfig, and AnomalyConfig, for the reason ADR 0009 established
// and ADR 0017 reapplied: each answers a different question. Policy
// answers "what decision should Trustvian make", Alert answers "which
// Results should notify", Anomaly answers "how should deviation be
// scored", and this document answers a fourth — "at what residual
// distrust does a Result become Medium, High, or Critical risk".
//
// It exists for the same reason AnomalyConfig does: before it, an
// external caller had no way to construct the value
// trustvian.WithTrustConfig accepts, because that parameter's type
// lives under internal/. The option was exported but unusable from
// outside this module.

package config

// TrustSchemaVersionV1 is the only TrustConfig schema version this
// package currently accepts — independently versioned from the other
// configuration documents, for the reason each of those is independent
// of the others (see SchemaVersionV1's own doc comment).
const TrustSchemaVersionV1 = "v1"

// TrustConfig is the public, declarative description of the risk
// thresholds Trustvian applies to a computed trust score — compiled and
// validated by CompileTrust into the value trustvian.WithTrustConfig
// accepts.
//
// The three thresholds are ascending cutoffs on *residual distrust*
// (`1 - TrustScore`), not on the trust score itself. A residual at or
// above CriticalThreshold is Critical risk; at or above HighThreshold
// but below Critical is High; at or above MediumThreshold but below
// High is Medium; anything lower is Low. Raising a threshold makes that
// risk level harder to reach.
//
// All three are pointers, and nil means "use the built-in default"
// (0.25, 0.5, 0.75 respectively). This follows the same rule
// AnomalyConfig's own non-zero-defaulted fields follow: a plain float64
// would make omitting a threshold indistinguishable from setting it to
// zero, and a zero threshold classifies *every* Result as Critical.
// Silently doing that to a caller who set only one of the three would
// be exactly the kind of quiet security-relevant surprise this
// package's zero-value discipline exists to prevent.
//
// CompileTrust(TrustConfig{Version: TrustSchemaVersionV1}) therefore
// produces the documented defaults, unchanged.
type TrustConfig struct {
	// Version must be TrustSchemaVersionV1 today. Required, not
	// defaulted: an empty or unrecognized Version fails validation
	// rather than silently assuming the current schema.
	Version string `yaml:"version"`

	// MediumThreshold is the residual-distrust cutoff at which a
	// Result becomes Medium risk. nil uses the default, 0.25.
	MediumThreshold *float64 `yaml:"medium_threshold,omitempty"`

	// HighThreshold is the cutoff for High risk. nil uses the
	// default, 0.5.
	HighThreshold *float64 `yaml:"high_threshold,omitempty"`

	// CriticalThreshold is the cutoff for Critical risk. nil uses the
	// default, 0.75.
	CriticalThreshold *float64 `yaml:"critical_threshold,omitempty"`
}
