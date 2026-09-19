package trustvian_test

// Task 048: WithContextRisk's callback takes a public StableFeatures,
// so a third-party module can write one. These tests cover what the
// callback is handed and that nothing about default scoring moved.
//
// The proof that it is genuinely usable from outside this module lives
// in examples/configured-engine, which is a separate Go module and
// cannot import internal packages at all.

import (
	"context"
	"testing"
	"time"

	"github.com/trustvian/trustvian"
	"github.com/trustvian/trustvian/event"
)

func secretsEvent() event.Event {
	return event.Event{
		ID:        "evt-1",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "svc-api", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /secrets"},
		Target:    event.Target{Name: "secrets-manager", Category: event.TargetCategoryExternal},
		Context:   event.Context{Environment: "production"},
	}
}

// TestContextRiskCallbackReceivesStableFeatures pins what the callback
// is given: the stable dimensions of the event, and only those.
func TestContextRiskCallbackReceivesStableFeatures(t *testing.T) {
	var got trustvian.StableFeatures
	calls := 0

	e := trustvian.NewEngine(trustvian.WithContextRisk(func(sf trustvian.StableFeatures) float64 {
		got = sf
		calls++
		return 0
	}))

	ev := secretsEvent()
	if _, err := e.Analyze(context.Background(), ev); err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("callback called %d times, want 1", calls)
	}

	want := trustvian.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryHTTP,
		OperationName:     "GET /secrets",
		TargetName:        "secrets-manager",
		TargetCategory:    event.TargetCategoryExternal,
		Environment:       "production",
	}
	if got != want {
		t.Fatalf("callback received %+v, want %+v", got, want)
	}
}

// TestContextRiskPenaltyReachesTrust proves the returned value is
// actually applied, so the option is wired end to end rather than
// merely accepted.
func TestContextRiskPenaltyReachesTrust(t *testing.T) {
	ctx := context.Background()
	ev := secretsEvent()

	base, err := trustvian.NewEngine().Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	penalised, err := trustvian.NewEngine(
		trustvian.WithContextRisk(func(sf trustvian.StableFeatures) float64 {
			if sf.TargetName == "secrets-manager" {
				return 0.5
			}
			return 0
		}),
	).Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if penalised.Trust.ContextRisk != 0.5 {
		t.Fatalf("Trust.ContextRisk = %v, want 0.5", penalised.Trust.ContextRisk)
	}
	if !(penalised.Trust.Score < base.Trust.Score) {
		t.Fatalf("context risk did not reduce trust: %v vs %v", penalised.Trust.Score, base.Trust.Score)
	}
}

// TestDefaultContextRiskIsZero is the no-regression half: an Engine
// constructed without the option scores exactly as it did before this
// option was made externally usable.
func TestDefaultContextRiskIsZero(t *testing.T) {
	r, err := trustvian.NewEngine().Analyze(context.Background(), secretsEvent())
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if r.Trust.ContextRisk != 0 {
		t.Fatalf("default Trust.ContextRisk = %v, want 0", r.Trust.ContextRisk)
	}
}
