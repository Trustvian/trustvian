package trustvian_test

import (
	"context"
	"fmt"
	"math"
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

// secretsToolPolicy blocks AI agents from touching tools whose
// tool.category attribute is "secrets" — the spec's own worked
// example for Condition.Attributes matching.
func secretsToolPolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{
				Name: "block-ai-agent-secrets-access",
				When: policy.Condition{
					ActorType:  event.ActorTypeAIAgent,
					Attributes: map[string]string{"tool.category": "secrets"},
				},
				Action: policy.DecisionBlock,
				Reason: "AI agents may not access secrets-category tools",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "no matching rule",
	}
}

func TestAnalyzeMatchesAttributeConditionEndToEnd(t *testing.T) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(secretsToolPolicy()))
	ctx := context.Background()

	ev := event.Event{
		ID:        "evt-secrets",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "agent-1",
			Type:               event.ActorTypeAIAgent,
			IdentityConfidence: 0.9,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryTool,
			Name:     "read-secret",
		},
		Target:  event.Target{Name: "secrets-manager"},
		Context: event.Context{Environment: "production"},
		Attributes: map[string]any{
			"tool.category": "secrets",
		},
	}

	result, err := engine.Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q, want %q for an AI agent hitting a tool.category=secrets attribute", result.Decision, policy.DecisionBlock)
	}
}

// TestAnalyzeNegativeDurationDoesNotCorruptTrustScore is a security
// regression test (docs/tasks/012-security-tests.md): a negative
// duration_ms attribute is adversarial/malformed input that isn't
// rejected by event.Validate (only IdentityConfidence and enum fields are
// checked there), so it flows through features.Extract into
// internal/anomaly's latency z-score math as a negative time.Duration.
//
// This was traced through empirically rather than assumed safe: a
// negative current latency against a baseline with non-zero variance
// still yields a large but finite z-score ((currentNS-mean)/stddev is a
// well-defined finite division whenever stddev != 0, regardless of the
// sign of currentNS), which min(z/threshold, 1) then clamps to the
// signal's normal [0,1] range exactly like any other extreme deviation.
// No NaN or Inf propagates into Anomaly.Score or Trust.Score. This test
// pins that finding down as a regression rather than leaving it an
// implicit assumption.
func TestAnalyzeNegativeDurationDoesNotCorruptTrustScore(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))

	// Warm up with varying (but bounded) latencies so the baseline
	// accumulates non-zero LatencyVariance — a constant latency would
	// take latencySignal's nearZeroStdDev branch instead, which never
	// exercises the division this test is targeting.
	latencies := []float64{10, 12, 9, 11, 8, 13, 10, 9, 12, 11, 10, 9, 13, 8, 11, 10, 12, 9, 11, 10, 8, 13, 9, 11, 10, 12, 9, 11, 10, 12}
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, l := range latencies {
		clock = clock.Add(time.Second)
		result, err := engine.Analyze(ctx, paymentEventAt(l, fmt.Sprintf("warm-up-%d", i), clock))
		if err != nil {
			t.Fatalf("Analyze() warm-up %d: error = %v", i, err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe() warm-up %d: error = %v", i, err)
		}
	}

	// A negative duration_ms is not something a well-behaved producer
	// sends, but Validate does not reject it — this event must still be
	// handled safely all the way through Trust.Score.
	clock = clock.Add(time.Second)
	result, err := engine.Analyze(ctx, paymentEventAt(-500, "negative-duration", clock))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if math.IsNaN(result.Anomaly.Score) || math.IsInf(result.Anomaly.Score, 0) {
		t.Fatalf("Anomaly.Score = %v for a negative duration_ms, want a finite value in [0,1]", result.Anomaly.Score)
	}
	if result.Anomaly.Score < 0 || result.Anomaly.Score > 1 {
		t.Fatalf("Anomaly.Score = %v out of [0,1] range", result.Anomaly.Score)
	}
	if math.IsNaN(result.Trust.Score) || math.IsInf(result.Trust.Score, 0) {
		t.Fatalf("Trust.Score = %v for a negative duration_ms, want a finite value in [0,1]", result.Trust.Score)
	}
	if result.Trust.Score < 0 || result.Trust.Score > 1 {
		t.Fatalf("Trust.Score = %v out of [0,1] range", result.Trust.Score)
	}
}

// TestAnalyzeCrossActorIsolation proves, end-to-end through Engine, what
// baseline.Key's composite (ActorID, Environment) shape only implies by
// construction: two actors that produce an otherwise identical stable
// feature shape (same operation, same target, same environment) never
// share Baseline state. actor-a is matured over 30 observations; if
// actor-b's first-ever event for the exact same shape came back with any
// non-zero Confidence, that would mean actor-a's history leaked across
// the actor boundary.
func TestAnalyzeCrossActorIsolation(t *testing.T) {
	ctx := context.Background()
	e := trustvian.NewEngine()

	shape := func(actorID string) event.Event {
		return event.Event{
			ID: actorID + "-evt", Timestamp: time.Now(),
			Actor:     event.Actor{ID: actorID, Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /shared"},
			Target:    event.Target{Name: "shared-target"},
			Context:   event.Context{Environment: "prod"},
		}
	}

	for range 30 {
		r, err := e.Analyze(ctx, shape("actor-a"))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := e.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}

	// actor-b has never been observed for this identical shape — it must
	// still register full categorical novelty, proving actor-a's 30
	// observations never leaked into actor-b's Baseline.
	rB, err := e.Analyze(ctx, shape("actor-b"))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rB.Anomaly.Confidence != 0 {
		t.Errorf("actor-b Confidence = %v, want 0 (no baseline should exist yet — cross-actor leak?)", rB.Anomaly.Confidence)
	}
}

// TestAnalyzeLargeAttributesMapDoesNotPanic is a resource-exhaustion
// smoke test (docs/tasks/012-security-tests.md): a producer sending an
// Attributes map with an unusually large number of keys must not panic
// or error Analyze — only duration_ms/error are ever read out of it, so
// cost should stay proportional to what's actually consumed, not to the
// map's total size.
func TestAnalyzeLargeAttributesMapDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	e := trustvian.NewEngine()
	attrs := make(map[string]any, 100000)
	for i := range 100000 {
		attrs[fmt.Sprintf("key-%d", i)] = i
	}
	ev := event.Event{
		ID: "evt", Timestamp: time.Now(),
		Actor:      event.Actor{ID: "a", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation:  event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
		Attributes: attrs,
	}
	if _, err := e.Analyze(ctx, ev); err != nil {
		t.Fatalf("Analyze() error = %v, want nil (large Attributes must not error or panic)", err)
	}
}

// TestObserveUnboundedFingerprintsDoesNotPanic is a resource-exhaustion
// smoke test (docs/tasks/012-security-tests.md): a single actor
// generating thousands of distinct fingerprints (e.g. a unique operation
// name per call) must not panic or error Engine.Observe. store.InMemory
// has no eviction policy today — see docs/SECURITY.md's "Resource
// exhaustion" entry — so this test only asserts the safety property (no
// panic, no error), not a bound on memory growth; the growth curve itself
// is docs/tasks/011-performance.md's concern.
func TestObserveUnboundedFingerprintsDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	e := trustvian.NewEngine()
	for i := range 5000 {
		ev := event.Event{
			ID: fmt.Sprintf("evt-%d", i), Timestamp: time.Now(),
			Actor:     event.Actor{ID: "actor-flood", Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: fmt.Sprintf("GET /x/%d", i)},
			Context:   event.Context{Environment: "prod"},
		}
		r, err := e.Analyze(ctx, ev)
		if err != nil {
			t.Fatalf("Analyze() at i=%d: %v", i, err)
		}
		if _, err := e.Observe(ctx, r); err != nil {
			t.Fatalf("Observe() at i=%d: %v", i, err)
		}
	}
}
