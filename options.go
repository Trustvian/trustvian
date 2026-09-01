package trustvian

import (
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/store"
	"github.com/Trustvian/trustvian/internal/trust"
)

// Option configures an Engine at construction time.
type Option func(*Engine)

// WithStore replaces the Engine's Baseline persistence. The default is an
// in-memory store (see store.NewInMemory): baselines do not survive
// process restarts unless a persistent Store is supplied.
func WithStore(s store.Store) Option {
	return func(e *Engine) { e.store = s }
}

// WithPolicy replaces the Engine's Policy. The default policy has no
// rules and an ObserveOnly fallback — a conservative starting point that
// never silently allows, and never blocks, until rules are configured.
func WithPolicy(p policy.Policy) Option {
	return func(e *Engine) { e.policy = p }
}

// WithAnomalyConfig replaces the thresholds and weights Score combines
// anomaly signals with. See anomaly.DefaultConfig for the baseline
// values.
func WithAnomalyConfig(cfg anomaly.Config) Option {
	return func(e *Engine) { e.anomalyConfig = cfg }
}

// WithTrustConfig replaces the RiskLevel bucket thresholds Trust is
// evaluated with. See trust.DefaultConfig for the baseline values.
func WithTrustConfig(cfg trust.Config) Option {
	return func(e *Engine) { e.trustConfig = cfg }
}

// WithContextRisk replaces the function used to derive a deterministic
// ContextRisk penalty (see trust.Compute) from an event's stable
// features. The default always returns 0 (no context penalty). fn must
// be pure and safe for concurrent use — Engine calls it on every Analyze.
func WithContextRisk(fn func(features.StableFeatures) float64) Option {
	return func(e *Engine) { e.contextRisk = fn }
}
