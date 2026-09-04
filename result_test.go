package trustvian

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

func TestResultExplainContainsAllDecisionFields(t *testing.T) {
	ctx := context.Background()
	e := NewEngine(WithAnomalyConfig(anomaly.Config{
		MinObservations: 1, NoveltyWeight: 1, LatencyWeight: 0.6, ErrorWeight: 0.8, FrequencyWeight: 0.6,
		LatencyZThreshold: 3, FrequencyZThreshold: 3, SensitiveTargetFloor: map[string]float64{},
	}))
	ev := event.Event{
		ID: "e1", Timestamp: time.Now(),
		Actor:     event.Actor{ID: "a1", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
		Target:    event.Target{Name: "svc"},
	}
	result, err := e.Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	got := result.Explain()

	if !strings.Contains(got, string(result.Decision)) {
		t.Errorf("Explain() missing Decision %q:\n%s", result.Decision, got)
	}
	if !strings.Contains(got, fmt.Sprintf("%.2f", result.Trust.Score)) {
		t.Errorf("Explain() missing Trust.Score %.2f:\n%s", result.Trust.Score, got)
	}
	if !strings.Contains(got, string(result.Trust.Risk)) {
		t.Errorf("Explain() missing Trust.Risk %q:\n%s", result.Trust.Risk, got)
	}
	if !strings.Contains(got, fmt.Sprintf("%.2f", result.Anomaly.Score)) {
		t.Errorf("Explain() missing Anomaly.Score %.2f:\n%s", result.Anomaly.Score, got)
	}
	if !strings.Contains(got, result.Explanation.Reason) {
		t.Errorf("Explain() missing policy reason %q:\n%s", result.Explanation.Reason, got)
	}
	// A brand-new fingerprint fires categorical_novelty — assert at least
	// one contributing signal's name appears.
	if len(result.Anomaly.Contributors) == 0 {
		t.Fatal("test setup: expected at least one anomaly contributor on a cold-start event")
	}
	if !strings.Contains(got, result.Anomaly.Contributors[0].Name) {
		t.Errorf("Explain() missing contributor name %q:\n%s", result.Anomaly.Contributors[0].Name, got)
	}
}

func TestResultExplainNoContributorsDoesNotPanic(t *testing.T) {
	r := Result{
		Decision:    policy.DecisionAllow,
		Explanation: policy.Explanation{Reason: "default allow", MatchedDefault: true},
		Trust:       trust.Trust{Score: 1, Risk: trust.RiskLow, IdentityConfidence: 1},
		Anomaly:     anomaly.Anomaly{Contributors: nil},
	}
	got := r.Explain() // must not panic on an empty Contributors slice
	if strings.Contains(got, "Detected:") {
		t.Error("Explain() rendered an empty Detected section for zero contributors")
	}
}
