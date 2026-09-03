package trustvian_test

import (
	"context"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/store"
	"github.com/Trustvian/trustvian/internal/trust"
)

func paymentEvent(latencyMS float64, id string) event.Event {
	return paymentEventAt(latencyMS, id, time.Now())
}

// paymentEventAt is paymentEvent with an explicit Timestamp. Tests that
// call it repeatedly to build up a mature baseline (see
// TestObserveLearnsOnlyFromEligibleDecisions and
// TestAnalyzeNormalBehaviorIsAllowed) must space those timestamps
// realistically (e.g. via a fixed clock stepped by a constant interval,
// not consecutive time.Now() calls within a tight Go loop) — the
// frequency_deviation signal (task 004) treats the wall-clock gap between
// consecutive calls as the actor's inter-request rate, and a tight loop's
// microsecond-scale, GC/scheduler-jittery gaps look nothing like a stable
// rate even though the loop is "doing the same thing" every iteration.
func paymentEventAt(latencyMS float64, id string, ts time.Time) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: ts,
		Actor: event.Actor{
			ID:                 "svc-payment",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.98,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryDB,
			Name:     "SELECT accounts",
		},
		Target:  event.Target{Name: "payment-db"},
		Context: event.Context{Environment: "production"},
		Attributes: map[string]any{
			"duration_ms": latencyMS,
		},
	}
}

// riskGatedPolicy blocks on high risk, alerts on medium, and otherwise
// allows — a realistic minimal policy for end-to-end tests.
func riskGatedPolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{Name: "block-high-risk", When: policy.Condition{MinRiskLevel: trust.RiskHigh}, Action: policy.DecisionBlock, Reason: "risk too high"},
			{Name: "alert-medium-risk", When: policy.Condition{MinRiskLevel: trust.RiskMedium}, Action: policy.DecisionAlert, Reason: "elevated risk"},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "risk within tolerance",
	}
}

func TestAnalyzeInvalidEventReturnsError(t *testing.T) {
	engine := trustvian.NewEngine()

	_, err := engine.Analyze(context.Background(), event.Event{})
	if err == nil {
		t.Fatalf("Analyze() error = nil for an invalid (zero-value) event, want an error")
	}
}

func TestAnalyzeIsReadOnly(t *testing.T) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	for i := range 10 {
		if _, err := engine.Analyze(ctx, paymentEvent(10, "evt")); err != nil {
			t.Fatalf("Analyze() call %d: error = %v", i, err)
		}
	}

	// Never called Observe: a fresh Analyze should behave exactly as the
	// very first one did (cold start), proving Analyze never wrote to
	// the Baseline.
	result, err := engine.Analyze(ctx, paymentEvent(10, "evt-final"))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Anomaly.Confidence != 0 {
		t.Fatalf("Anomaly.Confidence = %v after 10 Analyze-only calls, want 0 (Analyze must never learn)", result.Anomaly.Confidence)
	}
}

func TestObserveLearnsOnlyFromEligibleDecisions(t *testing.T) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	// Build up a mature, familiar baseline via Analyze+Observe: these
	// events are unremarkable, so risk stays low. Decision may
	// transiently be ALERT rather than ALLOW early on (partial maturity
	// still contributes some novelty signal) — that is expected and must
	// still be eligible for learning, or the fingerprint could never
	// mature past it. Only that it eventually settles to ALLOW matters.
	var result trustvian.Result
	var err error
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 30 {
		clock = clock.Add(time.Second)
		result, err = engine.Analyze(ctx, paymentEventAt(10, "warm-up", clock))
		if err != nil {
			t.Fatalf("Analyze() call %d: error = %v", i, err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe() call %d: error = %v", i, err)
		}
	}

	mature, err := engine.Analyze(ctx, paymentEventAt(10, "mature-check", clock.Add(time.Second)))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if mature.Anomaly.Confidence < 0.99 {
		t.Fatalf("Anomaly.Confidence = %v after 30 eligible Observe calls, want ~1", mature.Anomaly.Confidence)
	}
	if mature.Decision != policy.DecisionAllow {
		t.Fatalf("Decision = %q once fully mature, want %q (any transient ALERT during warm-up must settle)", mature.Decision, policy.DecisionAllow)
	}

	// Now feed a wildly anomalous event for a brand-new actor/target
	// that should be BLOCKed, and confirm Observe does NOT learn from
	// it: the fingerprint must stay entirely unknown afterward.
	blockedEvent := event.Event{
		ID:        "attack",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "svc-payment", Type: event.ActorTypeService, IdentityConfidence: 0.1},
		Operation: event.Operation{Category: event.OperationCategoryExternal, Name: "POST /exfiltrate"},
		Target:    event.Target{Name: "unknown-external-host"},
		Context:   event.Context{Environment: "production"},
	}
	blockedResult, err := engine.Analyze(ctx, blockedEvent)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if blockedResult.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q, want %q (test setup expects this event to be blocked)", blockedResult.Decision, policy.DecisionBlock)
	}
	learned, err := engine.Observe(ctx, blockedResult)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if learned {
		t.Fatalf("Observe() learned = true for a BLOCKed event, want false")
	}

	recheck, err := engine.Analyze(ctx, blockedEvent)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if recheck.Anomaly.Confidence != 0 {
		t.Fatalf("Anomaly.Confidence = %v after Observing a BLOCKed event, want 0 (must not have learned)", recheck.Anomaly.Confidence)
	}
}

func TestAnalyzeColdStartDoesNotFalselyBlock(t *testing.T) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))

	// First-ever event from a high-identity-confidence actor, no
	// history, no context risk: cold start should not push this into
	// BLOCK territory end-to-end.
	result, err := engine.Analyze(context.Background(), paymentEvent(10, "first-ever"))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Anomaly.Confidence != 0 {
		t.Fatalf("Anomaly.Confidence = %v for a first-ever event, want 0", result.Anomaly.Confidence)
	}
	if result.Decision == policy.DecisionBlock {
		t.Fatalf("Decision = %q for a benign first-ever event from a trusted identity, want anything but BLOCK", result.Decision)
	}
}

func TestAnalyzeSensitiveTargetFloorEndToEnd(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.SensitiveTargetFloor = map[string]float64{"secrets-manager": 0.9}

	secretsEvent := func(id string) event.Event {
		return event.Event{
			ID:        id,
			Timestamp: time.Now(),
			Actor:     event.Actor{ID: "svc-payment", Type: event.ActorTypeService, IdentityConfidence: 0.99},
			Operation: event.Operation{Category: event.OperationCategoryExternal, Name: "GET /secret"},
			Target:    event.Target{Name: "secrets-manager"},
			Context:   event.Context{Environment: "production"},
			Attributes: map[string]any{
				"duration_ms": float64(5),
			},
		}
	}

	// Make the fingerprint maximally familiar by seeding the store
	// directly, not through engine.Observe's gated learning loop: a
	// SensitiveTargetFloor this high keeps this exact scenario at BLOCK
	// even once mature (that's what this test is checking), and BLOCK is
	// correctly ineligible for learning — so the gated loop could never
	// reach full maturity here by construction. That's Observe's
	// anti-poisoning gate working as intended, not a way to build this
	// fixture.
	sample := secretsEvent("seed")
	feat := features.Extract(sample)
	fp := fingerprint.Compute(feat.Stable)
	key := baseline.Key{ActorID: sample.Actor.ID, Environment: sample.Context.Environment}
	seededStore := store.NewInMemory()
	for range 50 {
		if _, err := seededStore.Observe(ctx, key, fp, feat.Volatile, time.Now()); err != nil {
			t.Fatalf("seed Observe() error = %v", err)
		}
	}

	engine := trustvian.NewEngine(
		trustvian.WithStore(seededStore),
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(anomalyCfg),
	)

	final, err := engine.Analyze(ctx, secretsEvent("final"))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if final.Anomaly.Confidence < 0.99 {
		t.Fatalf("Anomaly.Confidence = %v, want ~1 (this scenario is deliberately maximally familiar)", final.Anomaly.Confidence)
	}
	if final.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q for a sensitive-target access, want %q despite full familiarity", final.Decision, policy.DecisionBlock)
	}
	if final.Explanation.Reason == "" {
		t.Fatalf("Explanation.Reason is empty")
	}
}

func TestAnalyzeNormalBehaviorIsAllowed(t *testing.T) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 30 {
		clock = clock.Add(time.Second)
		result, err := engine.Analyze(ctx, paymentEventAt(10, "warm-up", clock))
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
	}

	result, err := engine.Analyze(ctx, paymentEventAt(10, "steady-state", clock.Add(time.Second)))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Decision != policy.DecisionAllow {
		t.Fatalf("Decision = %q for behavior matching a mature baseline, want %q", result.Decision, policy.DecisionAllow)
	}
	if result.Trust.Risk != trust.RiskLow {
		t.Fatalf("Trust.Risk = %q, want %q", result.Trust.Risk, trust.RiskLow)
	}
}

func TestNewEngineDefaultsProduceValidResults(t *testing.T) {
	engine := trustvian.NewEngine() // no options at all

	result, err := engine.Analyze(context.Background(), paymentEvent(10, "defaults"))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Decision == "" {
		t.Fatalf("Decision is empty with default configuration")
	}
	if result.Explanation.Reason == "" {
		t.Fatalf("Explanation.Reason is empty with default configuration")
	}
}

func TestWithStoreUsesProvidedStore(t *testing.T) {
	custom := &countingStore{}
	engine := trustvian.NewEngine(trustvian.WithStore(custom))
	ctx := context.Background()

	result, err := engine.Analyze(ctx, paymentEvent(10, "evt"))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if custom.gets != 1 {
		t.Fatalf("custom store Get calls = %d, want 1 (Analyze must use the configured Store)", custom.gets)
	}

	if _, err := engine.Observe(ctx, result); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if custom.observes != 1 {
		t.Fatalf("custom store Observe calls = %d, want 1", custom.observes)
	}
}
