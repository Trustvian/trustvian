package trustvian_test

// Task 036 §37–§40: storage choice must not change behavior.
//
// Every other test in this package runs against the default in-memory
// store, which means the entire behavioral test suite — v0.5 policy, v0.6
// sequence analysis, v0.7 agent security, the learning-eligibility gate —
// is really a suite about one Store implementation. That is fine as long as
// backends are interchangeable, and worthless as evidence if they are not.
//
// These tests establish the interchangeability directly: the same event
// stream, driven through the same Engine pipeline, against InMemory,
// FileStore, and PostgreSQL, must produce the same learned Baseline and the
// same Anomaly/Trust/Decision. If that holds, every other test in this
// package transfers to the production backend; if it ever stops holding,
// something has introduced storage-dependent scoring, which is a defect no
// matter which backend "wins".
//
// PostgreSQL participates only when TRUSTVIAN_TEST_POSTGRES_DSN is set;
// InMemory and FileStore always run, so the comparison is never vacuous
// even without a database.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	trustvian "github.com/trustvian/trustvian"
	"github.com/trustvian/trustvian/config"
	"github.com/trustvian/trustvian/event"
	"github.com/trustvian/trustvian/internal/policy"
	"github.com/trustvian/trustvian/internal/trust"
)

// backend names a Store implementation together with a way to build an
// Engine on it, so each test below can iterate over all available backends
// without knowing how any of them is constructed.
type backend struct {
	name string
	// newEngine returns an Engine and a cleanup func. Built through the
	// public config boundary (config.CompileStorage) rather than by reaching
	// for internal constructors, so these tests exercise the same path an
	// external consumer uses — and so a backend that is unreachable through
	// public configuration cannot silently pass.
	newEngine func(t *testing.T, opts ...trustvian.Option) (*trustvian.Engine, func())
}

// availableBackends returns every backend this environment can test.
// PostgreSQL is included only when a DSN is configured; the two local
// backends always are.
func availableBackends(t *testing.T) []backend {
	t.Helper()

	backends := []backend{
		{
			name: "InMemory",
			newEngine: func(t *testing.T, opts ...trustvian.Option) (*trustvian.Engine, func()) {
				t.Helper()
				return engineOnStorage(t, config.StorageConfig{
					Version: config.StorageSchemaVersionV1,
					Type:    config.StorageTypeMemory,
				}, opts...)
			},
		},
		{
			name: "FileStore",
			newEngine: func(t *testing.T, opts ...trustvian.Option) (*trustvian.Engine, func()) {
				t.Helper()
				return engineOnStorage(t, config.StorageConfig{
					Version: config.StorageSchemaVersionV1,
					Type:    config.StorageTypeFile,
					File:    &config.FileStorageConfig{Path: filepath.Join(t.TempDir(), "baseline.json")},
				}, opts...)
			},
		},
	}

	if dsn := os.Getenv("TRUSTVIAN_TEST_POSTGRES_DSN"); dsn != "" {
		backends = append(backends, backend{
			name: "PostgreSQL",
			newEngine: func(t *testing.T, opts ...trustvian.Option) (*trustvian.Engine, func()) {
				t.Helper()
				return engineOnStorage(t, config.StorageConfig{
					Version:  config.StorageSchemaVersionV1,
					Type:     config.StorageTypePostgres,
					Postgres: &config.PostgresStorageConfig{DSN: dsn},
				}, opts...)
			},
		})
	}
	return backends
}

// engineOnStorage compiles cfg and wires the resulting Store into an
// Engine, returning the lifecycle cleanup the public API documents.
func engineOnStorage(t *testing.T, cfg config.StorageConfig, opts ...trustvian.Option) (*trustvian.Engine, func()) {
	t.Helper()

	s, err := config.CompileStorage(cfg)
	if err != nil {
		t.Fatalf("CompileStorage(%s): %v", cfg.Type, err)
	}
	cleanup := func() {}
	if c, ok := s.(io.Closer); ok {
		cleanup = func() { _ = c.Close() }
	}
	return trustvian.NewEngine(append([]trustvian.Option{trustvian.WithStore(s)}, opts...)...), cleanup
}

// equivalenceActor gives each backend's run a distinct actor id, because
// PostgreSQL's database is shared across test runs while InMemory's and
// FileStore's state is not. Fingerprint identity includes the actor type
// but not the actor id, so this does not change what is being compared.
func equivalenceActor(prefix, backendName string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, backendName, time.Now().UnixNano())
}

// equivalenceEvent is the event stream all backends see. It deliberately
// exercises more than one behavioral dimension — distinct operations (so
// transitions and n-grams form), varying latency (so timing statistics
// accumulate), and an occasional error — because a backend could round-trip
// Fingerprints correctly while dropping sequence or timing state, and a
// single-dimension stream would not notice.
func equivalenceEvent(actorID, id string, step int, ts time.Time) event.Event {
	operations := []string{"SELECT accounts", "UPDATE balance", "SELECT ledger"}
	return event.Event{
		ID:        id,
		Timestamp: ts,
		Actor: event.Actor{
			ID:                 actorID,
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryDB,
			Name:     operations[step%len(operations)],
		},
		Target:  event.Target{Name: "payment-db"},
		Context: event.Context{Environment: "production"},
		Attributes: map[string]any{
			"duration_ms": 10 + step%5,
			"error":       step%17 == 0,
		},
	}
}

// TestStorageBackendsLearnEquivalentBaselines is §37: identical input must
// produce identical learned state on every backend.
//
// The comparison is made on what the *pipeline reads back* — the Anomaly
// and Trust values a subsequent Analyze produces — rather than by
// reflecting over Baseline internals. That is the stricter and more
// meaningful test: it fails if any persisted field that influences scoring
// differs, and it does not fail over a field nothing reads. Two backends
// that agree here are interchangeable in the only sense that matters.
func TestStorageBackendsLearnEquivalentBaselines(t *testing.T) {
	backends := availableBackends(t)
	if len(backends) < 2 {
		t.Fatal("fewer than two backends available — this test cannot compare anything")
	}

	// equivalenceEvent cycles through three operations, so this yields ~30
	// observations per fingerprint — past the 20-observation maturity
	// threshold, so the comparison covers fully-mature state rather than
	// the partial-confidence path.
	const observations = 90
	type outcome struct {
		anomalyScore      float64
		anomalyConfidence float64
		trustScore        float64
		riskLevel         trust.RiskLevel
		decision          policy.Decision
		contributors      int
	}

	results := make(map[string]outcome, len(backends))

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			engine, cleanup := b.newEngine(t)
			defer cleanup()

			ctx := context.Background()
			actor := equivalenceActor("equiv", b.name)
			clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

			for i := range observations {
				clock = clock.Add(time.Minute)
				res, err := engine.Analyze(ctx, equivalenceEvent(actor, fmt.Sprintf("warm-%d", i), i, clock))
				if err != nil {
					t.Fatalf("Analyze(%d) error = %v", i, err)
				}
				if _, err := engine.Observe(ctx, res); err != nil {
					t.Fatalf("Observe(%d) error = %v", i, err)
				}
			}

			// One final Analyze reads the learned state back through the
			// whole pipeline.
			clock = clock.Add(time.Minute)
			final, err := engine.Analyze(ctx, equivalenceEvent(actor, "final", observations, clock))
			if err != nil {
				t.Fatalf("final Analyze() error = %v", err)
			}

			results[b.name] = outcome{
				anomalyScore:      final.Anomaly.Score,
				anomalyConfidence: final.Anomaly.Confidence,
				trustScore:        final.Trust.Score,
				riskLevel:         final.Trust.Risk,
				decision:          final.Decision,
				contributors:      len(final.Anomaly.Contributors),
			}
		})
	}

	// Compare every backend against the in-memory default, which is the
	// one every other test in this package implicitly uses.
	want, ok := results["InMemory"]
	if !ok {
		t.Fatal("InMemory backend did not run")
	}
	for name, got := range results {
		if name == "InMemory" {
			continue
		}
		if got != want {
			t.Errorf("%s produced a different outcome than InMemory after %d identical observations:\n  %s: %+v\n  InMemory: %+v\n"+
				"storage choice must not change behavioral scoring", name, observations, name, got, want)
		}
	}
	t.Logf("compared %d backends: %v", len(results), results)
}

// TestStorageBackendsProduceEquivalentDecisions is §38, extended across the
// decision spectrum rather than a single verdict. Each case drives a
// scenario that should land on a particular decision, and asserts every
// backend lands on the same one.
//
// Comparing decisions matters separately from comparing scores because the
// decision is what a deployment acts on. A small scoring difference that
// straddled a policy threshold would change a BLOCK into an ALLOW, which is
// a security-relevant divergence even though the numeric gap is tiny.
//
// Note what is and is not asserted. The assertion is cross-backend
// *equality*, never a particular decision — this test is not a detection-
// quality test and must not be read as one. The scenarios are chosen to
// land on visibly different decisions from each other (so agreement is
// meaningful rather than three backends trivially agreeing on ALLOW
// everywhere), and the logged decisions record what the engine currently
// does. Detection quality is v0.6/v0.7's territory and is covered by their
// own scenario tests.
func TestStorageBackendsProduceEquivalentDecisions(t *testing.T) {
	backends := availableBackends(t)
	if len(backends) < 2 {
		t.Fatal("fewer than two backends available — this test cannot compare anything")
	}

	scenarios := []struct {
		name string
		// warmUp is how many eligible observations to learn from first;
		// 0 means the probe event hits a completely cold baseline. 90
		// events across equivalenceEvent's three rotating operations is ~30
		// observations per fingerprint, which clears the 20-observation
		// maturity threshold — "after maturity" below is a literal claim,
		// not a loose one.
		warmUp int
		// probe builds the event whose decision is compared.
		probe func(actorID string, ts time.Time) event.Event
	}{
		{
			name:   "cold start, never-seen fingerprint",
			warmUp: 0,
			probe: func(actorID string, ts time.Time) event.Event {
				return equivalenceEvent(actorID, "cold-probe", 0, ts)
			},
		},
		{
			// Step 1, not step 0: equivalenceEvent marks every 17th step as
			// an error, so step 0 carries error=true and would fire the
			// error-deviation signal — making a "familiar" probe score high
			// for a reason that has nothing to do with familiarity.
			name:   "familiar behavior after maturity",
			warmUp: 90,
			probe: func(actorID string, ts time.Time) event.Event {
				return equivalenceEvent(actorID, "familiar-probe", 1, ts)
			},
		},
		{
			// Expect ALLOW here, and note why it is correct rather than a
			// missed detection: a brand-new fingerprint scores near-maximum
			// novelty but at zero *confidence*, and trust.Compute multiplies
			// the two (effectiveAnomaly = Score × Confidence). Cold start is
			// deliberately two numbers rather than one — see
			// docs/SECURITY.md § Cold start. What this case actually
			// contributes to task 036 is that all three backends agree on
			// that behavior.
			name:   "novel fingerprint from a known actor (cold-start path)",
			warmUp: 90,
			probe: func(actorID string, ts time.Time) event.Event {
				ev := equivalenceEvent(actorID, "novel-target-probe", 1, ts)
				ev.Target = event.Target{Name: "reporting-api", Category: event.TargetCategoryExternal}
				ev.Operation = event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /report"}
				return ev
			},
		},
		{
			name:   "extreme latency deviation on a mature fingerprint",
			warmUp: 90,
			probe: func(actorID string, ts time.Time) event.Event {
				ev := equivalenceEvent(actorID, "latency-probe", 1, ts)
				ev.Attributes = map[string]any{"duration_ms": 90000}
				return ev
			},
		},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			type verdict struct {
				decision  policy.Decision
				riskLevel trust.RiskLevel
				reason    string
			}
			got := make(map[string]verdict, len(backends))

			for _, b := range backends {
				engine, cleanup := b.newEngine(t, trustvian.WithPolicy(equivalencePolicy()))
				defer cleanup()

				ctx := context.Background()
				actor := equivalenceActor("decision", b.name)
				clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

				for i := range sc.warmUp {
					clock = clock.Add(time.Minute)
					res, err := engine.Analyze(ctx, equivalenceEvent(actor, fmt.Sprintf("warm-%d", i), i, clock))
					if err != nil {
						t.Fatalf("%s: Analyze(%d) error = %v", b.name, i, err)
					}
					if _, err := engine.Observe(ctx, res); err != nil {
						t.Fatalf("%s: Observe(%d) error = %v", b.name, i, err)
					}
				}

				clock = clock.Add(time.Minute)
				res, err := engine.Analyze(ctx, sc.probe(actor, clock))
				if err != nil {
					t.Fatalf("%s: probe Analyze() error = %v", b.name, err)
				}
				got[b.name] = verdict{
					decision:  res.Decision,
					riskLevel: res.Trust.Risk,
					reason:    res.Explanation.Reason,
				}
			}

			want := got["InMemory"]
			for name, v := range got {
				if name == "InMemory" {
					continue
				}
				if v != want {
					t.Errorf("%s decided %+v, InMemory decided %+v — a security decision must not depend on the storage backend",
						name, v, want)
				}
			}
			t.Logf("%s: all backends decided %q (%s)", sc.name, want.decision, want.riskLevel)
		})
	}
}

// equivalencePolicy is a risk-graduated policy, so the decision comparison
// above can actually distinguish outcomes. The default engine policy would
// collapse several scenarios onto one decision and weaken the test.
func equivalencePolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "block-critical",
				When:   policy.Condition{MinRiskLevel: trust.RiskCritical},
				Action: policy.DecisionBlock,
				Reason: "critical risk",
			},
			{
				Name:   "challenge-high",
				When:   policy.Condition{MinRiskLevel: trust.RiskHigh},
				Action: policy.DecisionChallenge,
				Reason: "high risk",
			},
			{
				Name:   "alert-medium",
				When:   policy.Condition{MinRiskLevel: trust.RiskMedium},
				Action: policy.DecisionAlert,
				Reason: "elevated risk",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "risk within tolerance",
	}
}

// TestLearningEligibilityGateHoldsOnEveryBackend is §39, the poisoning
// regression that matters most for this milestone.
//
// TestObserveLearnsOnlyFromEligibleDecisions already proves the gate works
// — against the in-memory store. Adding durable, *shared* persistence is
// exactly the change that could quietly undermine it: a backend that wrote
// on every Observe call regardless of eligibility would let an attacker
// normalize blocked behavior by repeating it, and would do so across every
// replica at once. The gate lives in Engine.Observe, above the Store, and
// this test asserts that placement holds for real rather than by
// inspection.
func TestLearningEligibilityGateHoldsOnEveryBackend(t *testing.T) {
	for _, b := range availableBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			engine, cleanup := b.newEngine(t, trustvian.WithPolicy(equivalencePolicy()))
			defer cleanup()

			ctx := context.Background()
			actor := equivalenceActor("poison", b.name)
			clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

			// A blocked, high-risk action against a sensitive target from a
			// completely unknown actor — the shape of an attack, and
			// ineligible for learning.
			attack := func(id string, ts time.Time) event.Event {
				return event.Event{
					ID:        id,
					Timestamp: ts,
					Actor: event.Actor{
						ID:                 actor,
						Type:               event.ActorTypeService,
						IdentityConfidence: 0.2,
					},
					Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /all-secrets"},
					Target:    event.Target{Name: "secrets-manager", Category: event.TargetCategoryExternal},
					Context:   event.Context{Environment: "production"},
				}
			}

			// Repeat it many times. If persistence bypassed the gate, the
			// fingerprint would accumulate observations and become familiar.
			var last trustvian.Result
			ineligible := 0
			for i := range 40 {
				clock = clock.Add(time.Minute)
				res, err := engine.Analyze(ctx, attack(fmt.Sprintf("attack-%d", i), clock))
				if err != nil {
					t.Fatalf("Analyze(%d) error = %v", i, err)
				}
				learned, err := engine.Observe(ctx, res)
				if err != nil {
					t.Fatalf("Observe(%d) error = %v", i, err)
				}
				if !learned {
					ineligible++
				}
				last = res
			}

			if ineligible == 0 {
				t.Fatalf("every one of 40 repetitions was learned — the eligibility gate did not engage, so this test proves nothing")
			}

			// The decisive assertion: after 40 repetitions the fingerprint is
			// still not trusted. Confidence stays at zero because nothing was
			// ever learned about it.
			if last.Anomaly.Confidence != 0 {
				t.Errorf("Anomaly.Confidence = %v after 40 ineligible repetitions, want 0 — "+
					"repeating a blocked action must never build familiarity", last.Anomaly.Confidence)
			}
			if last.Decision == policy.DecisionAllow {
				t.Errorf("Decision = %q after 40 repetitions, want it to remain non-ALLOW — "+
					"the baseline was poisoned through the %s backend", last.Decision, b.name)
			}
			t.Logf("%s: %d/40 repetitions correctly ineligible; final decision %q, confidence %v",
				b.name, ineligible, last.Decision, last.Anomaly.Confidence)
		})
	}
}

// TestAgentSecuritySemanticsHoldOnEveryBackend is §40: the v0.7 agent
// dimensions — delegation familiarity and approval evidence — must behave
// identically once state is persisted in a database.
//
// These are the newest behavioral signals and the ones whose state lives in
// the least-exercised corners of a serialized Baseline (DelegatorCounts in
// particular), so they are the most likely to be silently dropped by a
// storage layer. A dropped DelegatorCounts map would make every delegator
// look novel forever on PostgreSQL while looking familiar on InMemory.
func TestAgentSecuritySemanticsHoldOnEveryBackend(t *testing.T) {
	agentEvent := func(actorID, id, delegator string, approval event.ApprovalStatus, ts time.Time) event.Event {
		return event.Event{
			ID:        id,
			Timestamp: ts,
			Actor: event.Actor{
				ID:                 actorID,
				Type:               event.ActorTypeAIAgent,
				IdentityConfidence: 0.9,
			},
			Operation: event.Operation{Category: event.OperationCategoryTool, Name: "search_documents"},
			Target:    event.Target{Name: "document-index"},
			Context: event.Context{
				Environment:    "production",
				SessionID:      "session-1",
				DelegatedFrom:  delegator,
				ApprovalStatus: approval,
			},
		}
	}

	type outcome struct {
		familiarDelegatorScore float64
		novelDelegatorScore    float64
		confidence             float64
	}
	results := make(map[string]outcome)

	for _, b := range availableBackends(t) {
		t.Run(b.name, func(t *testing.T) {
			// Delegation scoring is opt-in, so it must be enabled explicitly
			// — exactly as an operator would through config.AnomalyConfig.
			anomalyCfg, err := config.CompileAnomaly(config.AnomalyConfig{
				Version:          config.AnomalySchemaVersionV1,
				DelegationWeight: 0.7,
			})
			if err != nil {
				t.Fatalf("CompileAnomaly: %v", err)
			}

			engine, cleanup := b.newEngine(t,
				trustvian.WithPolicy(equivalencePolicy()),
				trustvian.WithAnomalyConfig(anomalyCfg),
			)
			defer cleanup()

			ctx := context.Background()
			actor := equivalenceActor("agent", b.name)
			clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

			const warmUp = 40
			for i := range warmUp {
				clock = clock.Add(time.Minute)
				res, err := engine.Analyze(ctx, agentEvent(actor, fmt.Sprintf("agent-warm-%d", i), "orchestrator-a", event.ApprovalApproved, clock))
				if err != nil {
					t.Fatalf("Analyze(%d) error = %v", i, err)
				}
				if _, err := engine.Observe(ctx, res); err != nil {
					t.Fatalf("Observe(%d) error = %v", i, err)
				}
			}

			clock = clock.Add(time.Minute)
			familiar, err := engine.Analyze(ctx, agentEvent(actor, "familiar-delegator", "orchestrator-a", event.ApprovalApproved, clock))
			if err != nil {
				t.Fatalf("familiar Analyze() error = %v", err)
			}

			clock = clock.Add(time.Minute)
			novel, err := engine.Analyze(ctx, agentEvent(actor, "novel-delegator", "unknown-orchestrator", event.ApprovalApproved, clock))
			if err != nil {
				t.Fatalf("novel Analyze() error = %v", err)
			}

			// The signal must actually be doing something, or comparing
			// backends proves nothing.
			if novel.Anomaly.Score <= familiar.Anomaly.Score {
				t.Fatalf("novel delegator scored %v, familiar scored %v — delegation signal did not engage, so the comparison is vacuous",
					novel.Anomaly.Score, familiar.Anomaly.Score)
			}

			results[b.name] = outcome{
				familiarDelegatorScore: familiar.Anomaly.Score,
				novelDelegatorScore:    novel.Anomaly.Score,
				confidence:             familiar.Anomaly.Confidence,
			}
		})
	}

	want, ok := results["InMemory"]
	if !ok {
		t.Fatal("InMemory backend did not run")
	}
	for name, got := range results {
		if name == "InMemory" {
			continue
		}
		if got != want {
			t.Errorf("%s agent-security outcome %+v differs from InMemory %+v — "+
				"delegation state did not survive this backend identically", name, got, want)
		}
	}
	t.Logf("agent-security equivalence across %d backends: %+v", len(results), results)
}
