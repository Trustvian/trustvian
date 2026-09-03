package trustvian

import (
	"context"
	"fmt"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/store"
	"github.com/Trustvian/trustvian/internal/trust"
)

// Engine is the composition root of the Trustvian pipeline: Event ->
// Features -> Fingerprint -> Baseline -> Anomaly -> Trust -> Policy ->
// Decision. Construct one with NewEngine; it is safe for concurrent use
// by multiple goroutines once constructed, since nothing mutates an
// Engine's fields after NewEngine returns.
//
// Today, fully customizing an Engine (WithStore, WithPolicy,
// WithAnomalyConfig, WithTrustConfig, WithContextRisk) requires code
// living inside this module: their parameter types come from internal
// packages, which Go does not allow a separate module to import. A
// third-party caller can still construct Events, call Analyze and
// Observe, and read every field of Result — the full read path works
// with no restriction. Only supplying custom configuration from outside
// this module does not, yet. Promoting Policy/Config to a public package
// is a reasonable next step once an external consumer actually needs it;
// doing so pre-emptively, with no such consumer yet, would be exactly
// the speculative abstraction CLAUDE.md says to avoid.
type Engine struct {
	store         store.Store
	policy        policy.Policy
	anomalyConfig anomaly.Config
	trustConfig   trust.Config
	contextRisk   func(features.StableFeatures) float64
}

// NewEngine constructs an Engine, applying opts in order over sane
// defaults: an in-memory Store, a no-rules/ObserveOnly-default Policy,
// anomaly.DefaultConfig, trust.DefaultConfig, and a ContextRisk function
// that always returns 0.
func NewEngine(opts ...Option) *Engine {
	e := &Engine{
		store: store.NewInMemory(),
		policy: policy.Policy{
			DefaultAction: policy.DecisionObserveOnly,
			DefaultReason: "no policy rules configured; observing by default",
		},
		anomalyConfig: anomaly.DefaultConfig(),
		trustConfig:   trust.DefaultConfig(),
		contextRisk:   func(features.StableFeatures) float64 { return 0 },
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Analyze runs ev through the full pipeline and returns a Result. It is
// read-only: it never modifies the Engine's Baseline. Call Observe with
// the returned Result to (conditionally) learn from ev.
func (e *Engine) Analyze(ctx context.Context, ev event.Event) (Result, error) {
	if err := ev.Validate(); err != nil {
		return Result{}, fmt.Errorf("trustvian: invalid event: %w", err)
	}

	feat := features.Extract(ev)
	fp := fingerprint.Compute(feat.Stable)
	key := baseline.Key{ActorID: ev.Actor.ID, Environment: ev.Context.Environment}

	bl, _ := e.store.Get(ctx, key)
	an := anomaly.Score(feat, fp, bl, e.anomalyConfig)
	tr := trust.Compute(an, ev.Actor.IdentityConfidence, e.contextRisk(feat.Stable), e.trustConfig)
	pr := e.policy.Evaluate(policy.Input{Stable: feat.Stable, Trust: tr})

	return Result{
		Event:       ev,
		Features:    feat,
		Fingerprint: fp,
		BaselineKey: key,
		Anomaly:     an,
		Trust:       tr,
		Decision:    pr.Decision,
		Explanation: pr.Explanation,
	}, nil
}

// eligibleForLearning reports whether a Decision indicates the event it
// was computed for is safe to fold into the Baseline. The dividing line
// is whether the action proceeded: ALLOW, OBSERVE_ONLY and ALERT all let
// the event through (ALERT only adds visibility), so learning from them
// is safe. CHALLENGE, REQUIRE_APPROVAL and BLOCK all hold or stop the
// action pending confirmation, so they are excluded.
//
// ALERT deliberately stays eligible, not just ALLOW/OBSERVE_ONLY: a
// brand-new but ultimately benign Fingerprint can transiently cross into
// ALERT-level risk purely from partial maturity (see
// TestObserveLearnsOnlyFromEligibleDecisions) — categorical novelty is
// still ramping down even though nothing is actually wrong. Excluding
// ALERT from learning would create a deadlock: the very observations
// needed to mature the Fingerprint past that transient risk are the ones
// being rejected for not being mature enough yet, so it would stay
// ALERT-flagged forever instead of settling once familiar.
func eligibleForLearning(d policy.Decision) bool {
	switch d {
	case policy.DecisionAllow, policy.DecisionObserveOnly, policy.DecisionAlert:
		return true
	default:
		return false
	}
}

// Observe conditionally applies result to the Engine's Baseline: it is a
// no-op unless result.Decision is eligible for learning (see
// eligibleForLearning), reported via the returned learned bool. This is
// what makes Observe safe to call unconditionally after every Analyze —
// the gating that prevents baseline poisoning lives here, not in caller
// discipline.
//
// result must have been produced by this Engine's Analyze (or one
// configured identically); Observe trusts its Fingerprint, Features, and
// BaselineKey rather than recomputing them.
func (e *Engine) Observe(ctx context.Context, result Result) (learned bool, err error) {
	if !eligibleForLearning(result.Decision) {
		return false, nil
	}
	if _, err := e.store.Observe(ctx, result.BaselineKey, result.Fingerprint, result.Features.Volatile, result.Event.Timestamp); err != nil {
		return false, err
	}
	return true, nil
}
