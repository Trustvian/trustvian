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

// TestAnalyzeOrdinaryCadenceJitterDoesNotElevateRisk is the end-to-end
// counterpart to internal/anomaly's TestScoreFrequencyDeviationZScorePath.
// TestAnalyzeNormalBehaviorIsAllowed above learns from a *perfectly* flat
// one-second cadence, which no real caller produces; this one learns from
// a cadence with ordinary ±3ms jitter and then analyzes an event 5ms off
// the learned mean. Under the pre-fix defaults that ordinary event scored
// frequency_deviation ~0.79 × weight 0.6, reaching RiskMedium and firing
// the alert rule — roughly one in every five to ten wholly unremarkable
// events. It stays RiskLow now that FrequencyWeight defaults to 0.
func TestAnalyzeOrdinaryCadenceJitterDoesNotElevateRisk(t *testing.T) {
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	ctx := context.Background()

	// A fixed pattern, not real randomness, so this test is exactly
	// reproducible — it asserts on a z-score, which is meaningless if
	// the learned stddev drifts between runs.
	jitterMS := []int{3, -2, 1, -3, 2, 0, -1, 3, -3, 1, 2, -2, 0, -1, 1}

	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 50 {
		result, err := engine.Analyze(ctx, paymentEventAt(10, fmt.Sprintf("poll-%d", i), clock))
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
		clock = clock.Add(10*time.Second + time.Duration(jitterMS[i%len(jitterMS)])*time.Millisecond)
	}

	// One more entirely ordinary event: on cadence, 5ms off the learned
	// mean — well within what this actor's own jitter already produces.
	result, err := engine.Analyze(ctx, paymentEventAt(10, "ordinary", clock.Add(5*time.Millisecond)))
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.Trust.Risk != trust.RiskLow {
		t.Fatalf("Trust.Risk = %q for an ordinary on-cadence event with realistic jitter, want %q (anomaly %.3f, contributors %+v)",
			result.Trust.Risk, trust.RiskLow, result.Anomaly.Score, result.Anomaly.Contributors)
	}
	if result.Decision != policy.DecisionAllow {
		t.Fatalf("Decision = %q for an ordinary on-cadence event with realistic jitter, want %q", result.Decision, policy.DecisionAllow)
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

// TestAnalyzeTransitionDeviationEndToEnd is v0.6's own foundational
// end-to-end test — the security scenario docs/tasks/025-sequence-analysis-foundation.md
// exists to make detectable: authenticate -> read -> update is this
// actor's normal path; authenticate -> read -> delete has never
// happened, even though "delete" itself is independently familiar (via
// a different predecessor, authenticate). Built through the real,
// gated Analyze+Observe loop — not a directly-seeded store — per
// .claude/rules/testing.md's "end-to-end tests are load-bearing"
// convention: every step here is ALLOW-shaped and genuinely
// constructible through the gated loop, so it must be, not seeded
// around it.
func TestAnalyzeTransitionDeviationEndToEnd(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.TransitionWeight = 0.7 // opt-in: default is 0

	actorEvent := func(id, operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        id,
			Timestamp: ts,
			Actor:     event.Actor{ID: "svc-crm", Type: event.ActorTypeService, IdentityConfidence: 0.98},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "customer-db"},
			Context:   event.Context{Environment: "production"},
			Attributes: map[string]any{
				"duration_ms": float64(5),
			},
		}
	}

	engine := trustvian.NewEngine(
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(anomalyCfg),
	)

	analyzeAndObserve := func(t *testing.T, id, operation string, ts time.Time) trustvian.Result {
		t.Helper()
		result, err := engine.Analyze(ctx, actorEvent(id, operation, ts))
		if err != nil {
			t.Fatalf("Analyze(%s) error = %v", operation, err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe(%s) error = %v", operation, err)
		}
		return result
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	// Familiarize "delete" itself via an unrelated predecessor
	// (authenticate), so it is not novel on its own — isolating what
	// this test actually checks: the *transition* into it, not the
	// destination's own maturity.
	for i := range 25 {
		analyzeAndObserve(t, fmt.Sprintf("auth-%d", i), "authenticate", step())
		analyzeAndObserve(t, fmt.Sprintf("delete-seed-%d", i), "DELETE customer", step())
	}

	// This actor's actual normal path: read -> update, many times over.
	for i := range 25 {
		analyzeAndObserve(t, fmt.Sprintf("read-%d", i), "SELECT customer", step())
		analyzeAndObserve(t, fmt.Sprintf("update-%d", i), "UPDATE customer", step())
	}

	// One more read, observed, to become the real predecessor for the
	// event actually under test.
	analyzeAndObserve(t, "read-final", "SELECT customer", step())

	// read -> delete: never observed for this actor, even though
	// "delete" is independently familiar.
	deleteResult, err := engine.Analyze(ctx, actorEvent("delete-final", "DELETE customer", step()))
	if err != nil {
		t.Fatalf("Analyze(delete) error = %v", err)
	}

	var gotTransition, gotNovelty bool
	for _, c := range deleteResult.Anomaly.Contributors {
		switch c.Name {
		case "transition_deviation":
			gotTransition = true
		case "categorical_novelty":
			gotNovelty = true
		}
	}
	if !gotTransition {
		t.Fatalf("Anomaly.Contributors = %+v, want transition_deviation for the never-observed read->delete transition", deleteResult.Anomaly.Contributors)
	}
	if gotNovelty {
		t.Fatalf("Anomaly.Contributors = %+v, want no categorical_novelty — \"delete\" itself is independently familiar via authenticate->delete", deleteResult.Anomaly.Contributors)
	}

	// The actor's actual normal path, analyzed the same way, must not
	// carry transition_deviation.
	analyzeAndObserve(t, "read-final-2", "SELECT customer", step())
	familiarResult, err := engine.Analyze(ctx, actorEvent("update-familiar", "UPDATE customer", step()))
	if err != nil {
		t.Fatalf("Analyze(familiar update) error = %v", err)
	}
	for _, c := range familiarResult.Anomaly.Contributors {
		if c.Name == "transition_deviation" {
			t.Fatalf("Anomaly.Contributors = %+v, want no transition_deviation for the familiar read->update transition", familiarResult.Anomaly.Contributors)
		}
	}
}

// TestAnalyzeTransitionRarityEndToEnd is task 026's own central
// integration proof: a real actor whose normal path (read -> update)
// is common, and whose read -> delete path is rare-but-not-unseen
// (both destinations independently mature via other predecessors, so
// categorical_novelty/transition_deviation do not mask the rarity
// signal specifically under test), built entirely through the real,
// gated Analyze+Observe loop, flows all the way through Trust and
// Policy to a stricter Decision than the common path receives.
func TestAnalyzeTransitionRarityEndToEnd(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.TransitionRarityWeight = 0.9
	anomalyCfg.MinTransitionObservations = 20

	actorEvent := func(id, operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        id,
			Timestamp: ts,
			Actor:     event.Actor{ID: "svc-crm", Type: event.ActorTypeService, IdentityConfidence: 0.98},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "customer-db"},
			Context:   event.Context{Environment: "production"},
			Attributes: map[string]any{
				"duration_ms": float64(5),
			},
		}
	}

	engine := trustvian.NewEngine(
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(anomalyCfg),
	)

	analyzeAndObserve := func(t *testing.T, id, operation string, ts time.Time) trustvian.Result {
		t.Helper()
		result, err := engine.Analyze(ctx, actorEvent(id, operation, ts))
		if err != nil {
			t.Fatalf("Analyze(%s) error = %v", operation, err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe(%s) error = %v", operation, err)
		}
		return result
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	// Independently familiarize "delete" via authenticate -> delete,
	// so it is not novel on its own — isolating rarity specifically.
	for i := range 25 {
		analyzeAndObserve(t, fmt.Sprintf("auth-%d", i), "authenticate", step())
		analyzeAndObserve(t, fmt.Sprintf("delete-seed-%d", i), "DELETE customer", step())
	}

	// This actor's real distribution: read -> update 95 times (common),
	// read -> delete 5 times (rare) — 100 total outgoing transitions
	// from read, well past MinTransitionObservations.
	for i := range 95 {
		analyzeAndObserve(t, fmt.Sprintf("read-common-%d", i), "SELECT customer", step())
		analyzeAndObserve(t, fmt.Sprintf("update-%d", i), "UPDATE customer", step())
	}
	for i := range 4 { // the 5th read->delete is the event under test
		analyzeAndObserve(t, fmt.Sprintf("read-rare-%d", i), "SELECT customer", step())
		analyzeAndObserve(t, fmt.Sprintf("delete-rare-%d", i), "DELETE customer", step())
	}

	// One more read, observed, as the real predecessor for the events
	// actually under test.
	analyzeAndObserve(t, "read-final", "SELECT customer", step())

	rareResult, err := engine.Analyze(ctx, actorEvent("delete-final", "DELETE customer", step()))
	if err != nil {
		t.Fatalf("Analyze(rare delete) error = %v", err)
	}
	var rareRarity float64
	foundRarity := false
	for _, c := range rareResult.Anomaly.Contributors {
		if c.Name == "transition_rarity" {
			rareRarity = c.Value
			foundRarity = true
		}
		if c.Name == "transition_deviation" {
			t.Fatalf("Anomaly.Contributors = %+v, want no transition_deviation — read->delete has been observed (5/100), just rarely", rareResult.Anomaly.Contributors)
		}
	}
	if !foundRarity {
		t.Fatalf("Anomaly.Contributors = %+v, want transition_rarity for a ~5%% transition frequency", rareResult.Anomaly.Contributors)
	}
	// The exact count is not asserted here: riskGatedPolicy() means some
	// warm-up observations may transiently score high enough to be
	// BLOCKed/ineligible for learning (see eligibleForLearning), so the
	// precise accumulated denominator is not deterministic from this
	// test's own setup alone. The exact formula is proven, with a
	// controlled fixture, by TestScoreMatchesDocumentedFormulaForTransitionRarity
	// in internal/anomaly/anomaly_test.go — this test only needs the
	// qualitative property that a ~1-in-20 transition reads as clearly
	// rare.
	if rareRarity < 0.5 {
		t.Fatalf("transition_rarity Value = %v, want > 0.5 (a roughly 1-in-20 transition should read as clearly rare)", rareRarity)
	}

	// The common path, analyzed the same way, must carry a much lower
	// (or absent) transition_rarity, and a strictly lower Trust.Score-derived
	// risk than the rare path.
	analyzeAndObserve(t, "read-final-2", "SELECT customer", step())
	commonResult, err := engine.Analyze(ctx, actorEvent("update-common", "UPDATE customer", step()))
	if err != nil {
		t.Fatalf("Analyze(common update) error = %v", err)
	}
	var commonRarity float64
	for _, c := range commonResult.Anomaly.Contributors {
		if c.Name == "transition_rarity" {
			commonRarity = c.Value
		}
	}
	if commonRarity >= rareRarity {
		t.Fatalf("common transition_rarity (%v) >= rare transition_rarity (%v), want strictly less", commonRarity, rareRarity)
	}
	if !rareResult.Trust.Risk.AtLeast(commonResult.Trust.Risk) {
		t.Errorf("rare path Risk = %q, common path Risk = %q — want the rare path at least as risky", rareResult.Trust.Risk, commonResult.Trust.Risk)
	}
}

// TestAnalyzeTransitionRarityCrossActorIsolation proves Actor A's
// learned transition frequency never leaks into Actor B's scoring for
// the nominally identical transition — the same isolation guarantee
// TestAnalyzeCrossActorIsolation already proves for categorical
// novelty, restated here for the new, actor-scoped
// OutgoingTransitionTotal/PredecessorCounts state.
func TestAnalyzeTransitionRarityCrossActorIsolation(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.TransitionRarityWeight = 0.9
	anomalyCfg.MinTransitionObservations = 20

	engine := trustvian.NewEngine(
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(anomalyCfg),
	)

	shape := func(actorID, operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        actorID + "-" + operation + "-" + ts.String(),
			Timestamp: ts,
			Actor:     event.Actor{ID: actorID, Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "shared-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	// actor-a: read -> update is extremely common (30 times).
	for range 30 {
		r, err := engine.Analyze(ctx, shape("actor-a", "read", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := engine.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
		r, err = engine.Analyze(ctx, shape("actor-a", "update", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := engine.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}

	// actor-b has never been observed at all — the identical
	// read -> update transition, for actor-b, must show no rarity
	// evidence (no predecessor exists yet for a fresh actor).
	rB, err := engine.Analyze(ctx, shape("actor-b", "read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := engine.Observe(ctx, rB); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	rB2, err := engine.Analyze(ctx, shape("actor-b", "update", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if hasResultSignal(rB2, "transition_rarity") {
		t.Errorf("actor-b Contributors = %+v, want no transition_rarity — actor-a's 30 observations must not leak into actor-b's baseline", rB2.Anomaly.Contributors)
	}
}

// TestAnalyzeTransitionRarityScoresBeforeLearning proves Analyze never
// mutates OutgoingTransitionTotal/PredecessorCounts — the transition-
// rarity analogue of TestAnalyzeIsReadOnly. Calling Analyze many times
// on the same rare transition, without ever calling Observe, must
// produce the exact same rarity reading every time.
func TestAnalyzeTransitionRarityScoresBeforeLearning(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.TransitionRarityWeight = 0.9
	anomalyCfg.MinTransitionObservations = 20

	engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(anomalyCfg))

	shape := func(operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        operation + "-" + ts.String(),
			Timestamp: ts,
			Actor:     event.Actor{ID: "svc-order-only-learning", Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "order-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	// Build a real 95/5 split via the gated loop, exactly as the main
	// end-to-end test does.
	for range 19 {
		r, err := engine.Analyze(ctx, shape("read", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		engine.Observe(ctx, r)
		r, err = engine.Analyze(ctx, shape("update", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		engine.Observe(ctx, r)
	}
	r, err := engine.Analyze(ctx, shape("read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)
	r, err = engine.Analyze(ctx, shape("delete", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)

	r, err = engine.Analyze(ctx, shape("read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)

	// Now Analyze the rare (read -> delete) transition repeatedly,
	// without ever calling Observe again.
	eventTime := step()
	var firstRarity float64
	for i := range 10 {
		result, err := engine.Analyze(ctx, shape("delete", eventTime))
		if err != nil {
			t.Fatalf("Analyze() call %d: error = %v", i, err)
		}
		var rarity float64
		found := false
		for _, c := range result.Anomaly.Contributors {
			if c.Name == "transition_rarity" {
				rarity = c.Value
				found = true
			}
		}
		if !found {
			t.Fatalf("call %d: Contributors = %+v, want transition_rarity", i, result.Anomaly.Contributors)
		}
		if i == 0 {
			firstRarity = rarity
		} else if diff := rarity - firstRarity; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("call %d: transition_rarity Value = %v, want %v (unchanged across repeated Analyze-only calls — Analyze must never learn)", i, rarity, firstRarity)
		}
	}
}

// TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions is task
// 026's own poisoning-guard regression test: an attacker repeating a
// BLOCKed transition must never be able to inflate
// OutgoingTransitionTotal/PredecessorCounts and "wear down" the
// transition-rarity signal, mirroring
// TestObserveLearnsOnlyFromEligibleDecisions's existing proof for the
// rest of the baseline. This is not new logic — it falls out entirely
// from Engine.Observe's existing eligibleForLearning gate, which
// Baseline.Observe (and therefore this task's new counters) sits
// behind unconditionally; this test exists to prove that inheritance
// held, not to add a new mechanism.
func TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))

	readEvent := func(id string, ts time.Time) event.Event {
		return event.Event{
			ID: id, Timestamp: ts,
			Actor:     event.Actor{ID: "svc-poison-test", Type: event.ActorTypeService, IdentityConfidence: 0.98},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: "SELECT accounts"},
			Target:    event.Target{Name: "accounts-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := engine.Analyze(ctx, readEvent("read-1", now))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := engine.Observe(ctx, r); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// A wildly anomalous event that should be BLOCKed, immediately
	// following the read above.
	blocked := event.Event{
		ID:        "attack",
		Timestamp: now.Add(time.Second),
		Actor:     event.Actor{ID: "svc-poison-test", Type: event.ActorTypeService, IdentityConfidence: 0.1},
		Operation: event.Operation{Category: event.OperationCategoryExternal, Name: "POST /exfiltrate"},
		Target:    event.Target{Name: "unknown-external-host"},
		Context:   event.Context{Environment: "production"},
	}
	blockedResult, err := engine.Analyze(ctx, blocked)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if blockedResult.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q, want %q (test setup expects this event to be blocked)", blockedResult.Decision, policy.DecisionBlock)
	}

	// Repeat the BLOCKed transition many times — an attacker trying to
	// "train" read -> attack into looking common.
	for i := range 50 {
		attempt := blocked
		attempt.ID = fmt.Sprintf("attack-%d", i)
		attempt.Timestamp = now.Add(time.Duration(i+2) * time.Second)
		result, err := engine.Analyze(ctx, attempt)
		if err != nil {
			t.Fatalf("Analyze() attempt %d: error = %v", i, err)
		}
		if result.Decision != policy.DecisionBlock {
			t.Fatalf("attempt %d: Decision = %q, want %q", i, result.Decision, policy.DecisionBlock)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			t.Fatalf("Observe() attempt %d: error = %v", i, err)
		}
		if learned {
			t.Fatalf("attempt %d: Observe() learned = true for a BLOCKed event, want false", i)
		}
	}

	// The read -> attack transition must still read as entirely
	// unseen: 50 repeated BLOCKed attempts must have contributed zero
	// transition observations.
	recheck, err := engine.Analyze(ctx, blocked)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !hasResultSignal(recheck, "transition_deviation") {
		t.Errorf("Contributors = %+v, want transition_deviation still present (the transition must still read as entirely unseen after 50 BLOCKed attempts)", recheck.Anomaly.Contributors)
	}
	if hasResultSignal(recheck, "transition_rarity") {
		t.Errorf("Contributors = %+v, want no transition_rarity — a BLOCKed transition must never accumulate enough learned observations to become \"rare but seen\"", recheck.Anomaly.Contributors)
	}
}

// TestAnalyzeNGramEndToEnd is task 027's own central integration proof,
// built entirely through the real, gated Analyze+Observe loop (per
// .claude/rules/testing.md's "end-to-end tests are load-bearing"
// convention), reproducing the task's own canonical example:
// authenticate -> read_customer is a familiar pairwise transition (via
// one warm-up path), read_customer -> export_customer is *also* a
// familiar pairwise transition (via a *different* warm-up path), yet
// the complete sequence authenticate -> read_customer -> export_customer
// has never occurred — proving ngram_deviation detects this
// higher-order novelty through the full
// Event -> Engine -> Result -> Trust -> Policy -> Decision pipeline,
// not just in isolated unit tests.
func TestAnalyzeNGramEndToEnd(t *testing.T) {
	ctx := context.Background()

	actorEvent := func(id, operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        id,
			Timestamp: ts,
			Actor:     event.Actor{ID: "svc-crm", Type: event.ActorTypeService, IdentityConfidence: 0.98},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "customer-db"},
			Context:   event.Context{Environment: "production"},
			Attributes: map[string]any{
				"duration_ms": float64(5),
			},
		}
	}

	// Warm up with NGramWeight at its default (0) — the identical
	// "ships opt-in" precedent every other v0.6 signal already
	// established, applied here for a structural reason specific to
	// this signal: unlike transition_rarity (gated by minimum support,
	// so inert during early warm-up), ngram_deviation fires at full
	// strength on the *first* occurrence of any genuinely new 3-gram —
	// which is exactly what legitimate warm-up naturally produces.
	// Enabling the weight during warm-up would make the signal under
	// test BLOCK its own warm-up data (verified empirically: every
	// warm-up step introducing a fresh predecessor scored RiskCritical
	// and was excluded from learning), which is a warm-up-fixture
	// artifact, not something about the underlying detector this test
	// needs to prove. A shared store.Store lets two Engine instances
	// (identical policy, differing only in NGramWeight) observe the
	// same evolving Baseline: warmupEngine learns network the same way
	// any deployment's real early traffic would, without NGramWeight
	// paying its own opt-in cost during exactly that period; engine
	// then scores the events actually under test with the signal
	// enabled — the "operator raises the weight once calibrated"
	// sequence this codebase already documents for every prior
	// opt-in signal, just exercised across two Engine values sharing
	// one Store rather than one Engine value whose Config field
	// changes over time (which the immutable-Engine, functional-options
	// design does not support, deliberately).
	sharedStore := store.NewInMemory()
	warmupEngine := trustvian.NewEngine(trustvian.WithStore(sharedStore), trustvian.WithPolicy(riskGatedPolicy()))

	scoredCfg := anomaly.DefaultConfig()
	scoredCfg.NGramWeight = 0.9
	engine := trustvian.NewEngine(
		trustvian.WithStore(sharedStore),
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(scoredCfg),
	)

	analyzeAndObserve := func(t *testing.T, id, operation string, ts time.Time) trustvian.Result {
		t.Helper()
		result, err := warmupEngine.Analyze(ctx, actorEvent(id, operation, ts))
		if err != nil {
			t.Fatalf("Analyze(%s) error = %v", operation, err)
		}
		if _, err := warmupEngine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe(%s) error = %v", operation, err)
		}
		return result
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	// Familiarize authenticate -> read_customer, via
	// authenticate -> read_customer -> other_end (never export_customer).
	for i := range 20 {
		analyzeAndObserve(t, fmt.Sprintf("auth-%d", i), "authenticate", step())
		analyzeAndObserve(t, fmt.Sprintf("read-a-%d", i), "read_customer", step())
		analyzeAndObserve(t, fmt.Sprintf("other-end-%d", i), "other_end", step())
	}

	// Familiarize read_customer -> export_customer, via
	// other_start -> read_customer -> export_customer (never preceded
	// by authenticate).
	for i := range 20 {
		analyzeAndObserve(t, fmt.Sprintf("other-start-%d", i), "other_start", step())
		analyzeAndObserve(t, fmt.Sprintf("read-b-%d", i), "read_customer", step())
		analyzeAndObserve(t, fmt.Sprintf("export-seed-%d", i), "export_customer", step())
	}

	// Position the real history window at (authenticate, read_customer).
	analyzeAndObserve(t, "auth-final", "authenticate", step())
	analyzeAndObserve(t, "read-final", "read_customer", step())

	exportResult, err := engine.Analyze(ctx, actorEvent("export-final", "export_customer", step()))
	if err != nil {
		t.Fatalf("Analyze(export) error = %v", err)
	}
	if hasResultSignal(exportResult, "transition_deviation") {
		t.Fatalf("Contributors = %+v, want no transition_deviation — read_customer->export_customer is a familiar pairwise transition", exportResult.Anomaly.Contributors)
	}
	if !hasResultSignal(exportResult, "ngram_deviation") {
		t.Fatalf("Contributors = %+v, want ngram_deviation — the complete 3-gram authenticate->read_customer->export_customer was never observed", exportResult.Anomaly.Contributors)
	}

	// The alternative continuation actually trained for this exact
	// window (authenticate, read_customer) -> other_end must show no
	// such novelty, for comparison.
	otherEndResult, err := engine.Analyze(ctx, actorEvent("other-end-check", "other_end", step()))
	if err != nil {
		t.Fatalf("Analyze(other_end) error = %v", err)
	}
	if hasResultSignal(otherEndResult, "ngram_deviation") {
		t.Fatalf("Contributors = %+v, want no ngram_deviation — authenticate->read_customer->other_end has been observed 20 times", otherEndResult.Anomaly.Contributors)
	}

	if !exportResult.Trust.Risk.AtLeast(otherEndResult.Trust.Risk) {
		t.Errorf("export path Risk = %q, other_end path Risk = %q — want the novel-3-gram path at least as risky", exportResult.Trust.Risk, otherEndResult.Trust.Risk)
	}
}

// TestAnalyzeNGramCrossActorIsolation mirrors
// TestAnalyzeTransitionRarityCrossActorIsolation one level up: one
// actor's learned 3-gram history must never leak into a different
// actor's scoring for the nominally identical sequence.
func TestAnalyzeNGramCrossActorIsolation(t *testing.T) {
	ctx := context.Background()

	shape := func(actorID, operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        actorID + "-" + operation + "-" + ts.String(),
			Timestamp: ts,
			Actor:     event.Actor{ID: actorID, Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "shared-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	// See TestAnalyzeNGramEndToEnd's own comment for why warm-up uses
	// NGramWeight's default (0) and only the final scored call raises
	// it, via a second Engine sharing the same Store.
	sharedStore := store.NewInMemory()
	warmupEngine := trustvian.NewEngine(trustvian.WithStore(sharedStore), trustvian.WithPolicy(riskGatedPolicy()))
	scoredCfg := anomaly.DefaultConfig()
	scoredCfg.NGramWeight = 0.9
	engine := trustvian.NewEngine(
		trustvian.WithStore(sharedStore),
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(scoredCfg),
	)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	analyzeAndObserve := func(actorID, operation string) trustvian.Result {
		r, err := warmupEngine.Analyze(ctx, shape(actorID, operation, step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := warmupEngine.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
		return r
	}

	// actor-a: authenticate -> read -> update, trained 20 times.
	for range 20 {
		analyzeAndObserve("actor-a", "authenticate")
		analyzeAndObserve("actor-a", "read")
		analyzeAndObserve("actor-a", "update")
	}

	// actor-b has never been observed at all. Its own
	// authenticate -> read -> update *is* genuinely novel for actor-b's
	// own, fresh baseline — ngram_deviation firing here is correct, not
	// a bug (a 3-gram cannot be non-novel until it has actually been
	// observed for *this* actor). The isolation property this test
	// actually proves is narrower and more precise: the signal's Value
	// must read as *maximally* novel (1), not some intermediate value —
	// which is only possible if actor-a's 20 real observations of the
	// nominally identical sequence contributed nothing to actor-b's own
	// TrigramCounts.
	warmupR, err := warmupEngine.Analyze(ctx, shape("actor-b", "authenticate", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	warmupEngine.Observe(ctx, warmupR)
	warmupR, err = warmupEngine.Analyze(ctx, shape("actor-b", "read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	warmupEngine.Observe(ctx, warmupR)

	rB, err := engine.Analyze(ctx, shape("actor-b", "update", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	var ngramValue float64
	found := false
	for _, c := range rB.Anomaly.Contributors {
		if c.Name == "ngram_deviation" {
			ngramValue, found = c.Value, true
		}
	}
	if !found {
		t.Fatalf("actor-b Contributors = %+v, want ngram_deviation (a genuinely novel 3-gram for actor-b's own baseline)", rB.Anomaly.Contributors)
	}
	if ngramValue != 1 {
		t.Errorf("actor-b ngram_deviation Value = %v, want exactly 1 — anything less would mean actor-a's 20 observations leaked into actor-b's baseline", ngramValue)
	}
	if hasResultSignal(rB, "ngram_rarity") {
		t.Errorf("actor-b Contributors = %+v, want no ngram_rarity (mutually exclusive with ngram_deviation)", rB.Anomaly.Contributors)
	}
}

// TestAnalyzeNGramScoresBeforeLearning proves Analyze never mutates
// TrigramCounts/TrigramContinuationTotal — the 3-gram analogue of
// TestAnalyzeTransitionRarityScoresBeforeLearning. Calling Analyze many
// times on the same novel 3-gram, without ever calling Observe, must
// produce the exact same reading every time.
func TestAnalyzeNGramScoresBeforeLearning(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.NGramWeight = 0.9

	engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(anomalyCfg))

	shape := func(operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        operation + "-" + ts.String(),
			Timestamp: ts,
			Actor:     event.Actor{ID: "svc-ngram-only-learning", Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "order-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	for range 3 {
		r, err := engine.Analyze(ctx, shape("authenticate", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		engine.Observe(ctx, r)
	}
	r, err := engine.Analyze(ctx, shape("read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)

	// Now Analyze the novel (authenticate, ..., read) -> delete 3-gram
	// repeatedly, without ever calling Observe again.
	eventTime := step()
	var results []bool
	for range 10 {
		result, err := engine.Analyze(ctx, shape("delete", eventTime))
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}
		results = append(results, hasResultSignal(result, "ngram_deviation"))
	}
	for i, fired := range results {
		if !fired {
			t.Fatalf("call %d: ngram_deviation fired = false, want true (unchanged across repeated Analyze-only calls — Analyze must never learn)", i)
		}
	}
}

// TestObserveNGramLearnsOnlyFromEligibleDecisions is task 027's own
// poisoning-guard regression test, using the exact scenario the task's
// own brief names: a normal path (authenticate -> read -> update) and a
// malicious path (authenticate -> export -> delete) that gets BLOCKed.
// Repeatedly replaying the malicious sequence must never normalize it —
// this is not new logic, it falls out entirely from Engine.Observe's
// existing eligibleForLearning gate, which Baseline.Observe (and
// therefore this task's new TrigramCounts/TrigramContinuationTotal
// state) sits behind unconditionally; this test exists to prove that
// inheritance held, not to add a new mechanism.
func TestObserveNGramLearnsOnlyFromEligibleDecisions(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))

	normalEvent := func(id, operation string, ts time.Time) event.Event {
		return event.Event{
			ID: id, Timestamp: ts,
			Actor:     event.Actor{ID: "svc-ngram-poison-test", Type: event.ActorTypeService, IdentityConfidence: 0.98},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "customer-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Establish the history window at (authenticate, export) — the
	// setup step before the malicious "delete" completes the 3-gram
	// under test. "export" itself is unremarkable here (it is only the
	// *final* delete step that is wildly anomalous).
	r, err := engine.Analyze(ctx, normalEvent("auth-1", "authenticate", now))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := engine.Observe(ctx, r); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	r, err = engine.Analyze(ctx, normalEvent("export-1", "export_customer", now.Add(time.Second)))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := engine.Observe(ctx, r); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// A wildly anomalous event, immediately following authenticate ->
	// export_customer — the malicious 3-gram's completion.
	blocked := event.Event{
		ID:        "attack",
		Timestamp: now.Add(2 * time.Second),
		Actor:     event.Actor{ID: "svc-ngram-poison-test", Type: event.ActorTypeService, IdentityConfidence: 0.1},
		Operation: event.Operation{Category: event.OperationCategoryExternal, Name: "POST /exfiltrate"},
		Target:    event.Target{Name: "unknown-external-host"},
		Context:   event.Context{Environment: "production"},
	}
	blockedResult, err := engine.Analyze(ctx, blocked)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if blockedResult.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q, want %q (test setup expects this event to be blocked)", blockedResult.Decision, policy.DecisionBlock)
	}

	// Repeat the BLOCKed 3-gram many times — an attacker trying to
	// "train" authenticate -> export_customer -> attack into looking
	// familiar.
	for i := range 50 {
		attempt := blocked
		attempt.ID = fmt.Sprintf("attack-%d", i)
		attempt.Timestamp = now.Add(time.Duration(i+3) * time.Second)
		result, err := engine.Analyze(ctx, attempt)
		if err != nil {
			t.Fatalf("Analyze() attempt %d: error = %v", i, err)
		}
		if result.Decision != policy.DecisionBlock {
			t.Fatalf("attempt %d: Decision = %q, want %q", i, result.Decision, policy.DecisionBlock)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			t.Fatalf("Observe() attempt %d: error = %v", i, err)
		}
		if learned {
			t.Fatalf("attempt %d: Observe() learned = true for a BLOCKed event, want false", i)
		}
	}

	// The 3-gram must still read as entirely unseen: 50 repeated
	// BLOCKed attempts must have contributed zero learned 3-gram
	// observations.
	recheck, err := engine.Analyze(ctx, blocked)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !hasResultSignal(recheck, "ngram_deviation") {
		t.Errorf("Contributors = %+v, want ngram_deviation still present (the 3-gram must still read as entirely unseen after 50 BLOCKed attempts)", recheck.Anomaly.Contributors)
	}
	if hasResultSignal(recheck, "ngram_rarity") {
		t.Errorf("Contributors = %+v, want no ngram_rarity — a BLOCKed 3-gram must never accumulate enough learned observations to become \"rare but seen\"", recheck.Anomaly.Contributors)
	}
}

// TestAnalyzeMarkovCrossActorIsolation mirrors
// TestAnalyzeTransitionRarityCrossActorIsolation one level over:
// markov_surprisal reads the identical PredecessorCounts/
// OutgoingTransitionTotal state transition_rarity does, scoped by the
// same baseline.Key{ActorID, Environment} — one actor's learned
// transition frequency must never leak into a different actor's
// scoring for the nominally identical transition.
func TestAnalyzeMarkovCrossActorIsolation(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.MarkovWeight = 0.9

	engine := trustvian.NewEngine(
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(anomalyCfg),
	)

	shape := func(actorID, operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        actorID + "-" + operation + "-" + ts.String(),
			Timestamp: ts,
			Actor:     event.Actor{ID: actorID, Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "shared-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	// actor-a: read -> update is extremely common (30 times).
	for range 30 {
		r, err := engine.Analyze(ctx, shape("actor-a", "read", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := engine.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
		r, err = engine.Analyze(ctx, shape("actor-a", "update", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := engine.Observe(ctx, r); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}

	// actor-b has never been observed at all — the identical
	// read -> update transition, for actor-b, must show no Markov
	// evidence (no predecessor exists yet for a fresh actor).
	rB, err := engine.Analyze(ctx, shape("actor-b", "read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := engine.Observe(ctx, rB); err != nil {
		t.Fatalf("Observe: %v", err)
	}
	rB2, err := engine.Analyze(ctx, shape("actor-b", "update", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if hasResultSignal(rB2, "markov_surprisal") {
		t.Errorf("actor-b Contributors = %+v, want no markov_surprisal — actor-a's 30 observations must not leak into actor-b's baseline", rB2.Anomaly.Contributors)
	}
}

// TestAnalyzeMarkovScoresBeforeLearning proves Analyze never mutates
// PredecessorCounts/OutgoingTransitionTotal — the markov_surprisal
// analogue of TestAnalyzeTransitionRarityScoresBeforeLearning. Calling
// Analyze many times on the same rare transition, without ever calling
// Observe, must produce the exact same surprisal reading every time.
func TestAnalyzeMarkovScoresBeforeLearning(t *testing.T) {
	ctx := context.Background()
	anomalyCfg := anomaly.DefaultConfig()
	anomalyCfg.MarkovWeight = 0.9
	anomalyCfg.MinTransitionObservations = 20

	engine := trustvian.NewEngine(trustvian.WithAnomalyConfig(anomalyCfg))

	shape := func(operation string, ts time.Time) event.Event {
		return event.Event{
			ID:        operation + "-" + ts.String(),
			Timestamp: ts,
			Actor:     event.Actor{ID: "svc-markov-only-learning", Type: event.ActorTypeService, IdentityConfidence: 1},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: operation},
			Target:    event.Target{Name: "order-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	// Build a real 19/1 split via the gated loop, exactly as the
	// transition-rarity test does.
	for range 19 {
		r, err := engine.Analyze(ctx, shape("read", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		engine.Observe(ctx, r)
		r, err = engine.Analyze(ctx, shape("update", step()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		engine.Observe(ctx, r)
	}
	r, err := engine.Analyze(ctx, shape("read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)
	r, err = engine.Analyze(ctx, shape("delete", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)

	r, err = engine.Analyze(ctx, shape("read", step()))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	engine.Observe(ctx, r)

	// Now Analyze the rare (read -> delete) transition repeatedly,
	// without ever calling Observe again.
	eventTime := step()
	var firstSurprisal float64
	for i := range 10 {
		result, err := engine.Analyze(ctx, shape("delete", eventTime))
		if err != nil {
			t.Fatalf("Analyze() call %d: error = %v", i, err)
		}
		var surprisal float64
		found := false
		for _, c := range result.Anomaly.Contributors {
			if c.Name == "markov_surprisal" {
				surprisal, found = c.Value, true
			}
		}
		if !found {
			t.Fatalf("call %d: Contributors = %+v, want markov_surprisal", i, result.Anomaly.Contributors)
		}
		if i == 0 {
			firstSurprisal = surprisal
		} else if diff := surprisal - firstSurprisal; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("call %d: markov_surprisal Value = %v, want %v (unchanged across repeated Analyze-only calls — Analyze must never learn)", i, surprisal, firstSurprisal)
		}
	}
}

// TestObserveMarkovLearnsOnlyFromEligibleDecisions is task 028's own
// poisoning-guard regression test, mirroring
// TestObserveTransitionRarityLearnsOnlyFromEligibleDecisions one level
// over: not new logic — markov_surprisal reads the identical
// PredecessorCounts/OutgoingTransitionTotal state, which already sits
// behind Engine.Observe's eligibleForLearning gate; this test exists
// to prove that inheritance held for the new signal specifically.
func TestObserveMarkovLearnsOnlyFromEligibleDecisions(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))

	readEvent := func(id string, ts time.Time) event.Event {
		return event.Event{
			ID: id, Timestamp: ts,
			Actor:     event.Actor{ID: "svc-markov-poison-test", Type: event.ActorTypeService, IdentityConfidence: 0.98},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: "SELECT accounts"},
			Target:    event.Target{Name: "accounts-db"},
			Context:   event.Context{Environment: "production"},
		}
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := engine.Analyze(ctx, readEvent("read-1", now))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := engine.Observe(ctx, r); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// A wildly anomalous event that should be BLOCKed, immediately
	// following the read above.
	blocked := event.Event{
		ID:        "attack",
		Timestamp: now.Add(time.Second),
		Actor:     event.Actor{ID: "svc-markov-poison-test", Type: event.ActorTypeService, IdentityConfidence: 0.1},
		Operation: event.Operation{Category: event.OperationCategoryExternal, Name: "POST /exfiltrate"},
		Target:    event.Target{Name: "unknown-external-host"},
		Context:   event.Context{Environment: "production"},
	}
	blockedResult, err := engine.Analyze(ctx, blocked)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if blockedResult.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q, want %q (test setup expects this event to be blocked)", blockedResult.Decision, policy.DecisionBlock)
	}

	// Repeat the BLOCKed transition many times — an attacker trying to
	// "train" read -> attack into looking common.
	for i := range 50 {
		attempt := blocked
		attempt.ID = fmt.Sprintf("attack-%d", i)
		attempt.Timestamp = now.Add(time.Duration(i+2) * time.Second)
		result, err := engine.Analyze(ctx, attempt)
		if err != nil {
			t.Fatalf("Analyze() attempt %d: error = %v", i, err)
		}
		if result.Decision != policy.DecisionBlock {
			t.Fatalf("attempt %d: Decision = %q, want %q", i, result.Decision, policy.DecisionBlock)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			t.Fatalf("Observe() attempt %d: error = %v", i, err)
		}
		if learned {
			t.Fatalf("attempt %d: Observe() learned = true for a BLOCKed event, want false", i)
		}
	}

	// The read -> attack transition must still read as entirely
	// unseen: 50 repeated BLOCKed attempts must have contributed zero
	// transition observations.
	recheck, err := engine.Analyze(ctx, blocked)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !hasResultSignal(recheck, "transition_deviation") {
		t.Errorf("Contributors = %+v, want transition_deviation still present (the transition must still read as entirely unseen after 50 BLOCKed attempts)", recheck.Anomaly.Contributors)
	}
	if hasResultSignal(recheck, "markov_surprisal") {
		t.Errorf("Contributors = %+v, want no markov_surprisal — a BLOCKed transition must never accumulate enough learned observations to become \"familiar but rare\"", recheck.Anomaly.Contributors)
	}
}

func hasResultSignal(result trustvian.Result, name string) bool {
	for _, c := range result.Anomaly.Contributors {
		if c.Name == name {
			return true
		}
	}
	return false
}
