package alert_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/fingerprint"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

func sampleResult() trustvian.Result {
	ts := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	return trustvian.Result{
		Event: event.Event{
			ID:        "evt-1",
			Timestamp: ts,
			Actor: event.Actor{
				ID:                 "svc-payment",
				Type:               event.ActorTypeService,
				IdentityConfidence: 0.4,
			},
			Operation: event.Operation{Category: event.OperationCategoryDB, Name: "SELECT accounts"},
			Target:    event.Target{Name: "payment-db", Category: event.TargetCategoryDatabase},
			Context:   event.Context{Environment: "production"},
		},
		Fingerprint: fingerprint.Fingerprint{ID: "fp_abc123"},
		Anomaly: anomaly.Anomaly{
			Score:      0.9,
			Confidence: 1.0,
			Contributors: []anomaly.Signal{
				{Name: "categorical_novelty", Value: 0.9, Weight: 1.0, Detail: "fingerprint never observed for this actor"},
				{Name: "error_deviation", Value: 0.5, Weight: 1.0, Detail: ""},
			},
		},
		Trust:    trust.Trust{Score: 0.2, Risk: trust.RiskHigh},
		Decision: policy.DecisionBlock,
		Explanation: policy.Explanation{
			RuleName: "block-sensitive-target",
			Reason:   "sensitive destination at high risk",
		},
	}
}

func TestNewBuildsAlertFromResult(t *testing.T) {
	result := sampleResult()

	a := alert.New(result, alert.SeverityCritical)

	if a.ID == "" || !strings.HasPrefix(a.ID, "alt_") {
		t.Errorf("ID = %q, want a non-empty value with prefix %q", a.ID, "alt_")
	}
	if !a.Timestamp.Equal(result.Event.Timestamp) {
		t.Errorf("Timestamp = %v, want %v (Result.Event.Timestamp)", a.Timestamp, result.Event.Timestamp)
	}
	if a.Severity != alert.SeverityCritical {
		t.Errorf("Severity = %q, want %q", a.Severity, alert.SeverityCritical)
	}
	if a.Decision != result.Decision {
		t.Errorf("Decision = %q, want %q", a.Decision, result.Decision)
	}
	if a.Risk != result.Trust.Risk {
		t.Errorf("Risk = %q, want %q", a.Risk, result.Trust.Risk)
	}
	if a.TrustScore != result.Trust.Score {
		t.Errorf("TrustScore = %v, want %v", a.TrustScore, result.Trust.Score)
	}
	if a.AnomalyScore != result.Anomaly.Score {
		t.Errorf("AnomalyScore = %v, want %v", a.AnomalyScore, result.Anomaly.Score)
	}
	if a.Actor != result.Event.Actor {
		t.Errorf("Actor = %+v, want %+v", a.Actor, result.Event.Actor)
	}
	if a.Target != result.Event.Target {
		t.Errorf("Target = %+v, want %+v", a.Target, result.Event.Target)
	}
	if a.FingerprintID != result.Fingerprint.ID {
		t.Errorf("FingerprintID = %q, want %q", a.FingerprintID, result.Fingerprint.ID)
	}
}

func TestNewReasonsIncludeContributorsAndPolicyReason(t *testing.T) {
	result := sampleResult()

	a := alert.New(result, alert.SeverityHigh)

	want := []string{
		"fingerprint never observed for this actor", // Detail present
		"error_deviation", // no Detail -> falls back to Name
		`policy rule "block-sensitive-target": sensitive destination at high risk`,
	}
	if !reflect.DeepEqual(a.Reasons, want) {
		t.Errorf("Reasons = %#v, want %#v", a.Reasons, want)
	}
}

func TestNewReasonsUsesPolicyDefaultWording(t *testing.T) {
	result := sampleResult()
	result.Anomaly.Contributors = nil
	result.Explanation = policy.Explanation{MatchedDefault: true, Reason: "no policy rules configured; observing by default"}

	a := alert.New(result, alert.SeverityInfo)

	want := []string{"policy default: no policy rules configured; observing by default"}
	if !reflect.DeepEqual(a.Reasons, want) {
		t.Errorf("Reasons = %#v, want %#v", a.Reasons, want)
	}
}

func TestNewDoesNotMutateResult(t *testing.T) {
	result := sampleResult()
	before := deepCopyResult(result)

	_ = alert.New(result, alert.SeverityCritical)

	if !reflect.DeepEqual(result, before) {
		t.Errorf("New mutated its Result argument: got %+v, want %+v", result, before)
	}
}

// deepCopyResult copies the slice/map fields Result holds by reference
// so a before/after reflect.DeepEqual comparison actually proves nothing
// aliased was mutated in place, rather than comparing a value against
// itself.
func deepCopyResult(r trustvian.Result) trustvian.Result {
	cp := r
	cp.Anomaly.Contributors = append([]anomaly.Signal(nil), r.Anomaly.Contributors...)
	return cp
}

func TestNewIDsAreUniquePerCall(t *testing.T) {
	result := sampleResult()

	a1 := alert.New(result, alert.SeverityLow)
	a2 := alert.New(result, alert.SeverityLow)

	if a1.ID == a2.ID {
		t.Errorf("two New calls for the same Result produced the same ID %q", a1.ID)
	}
}

func TestSeverityValid(t *testing.T) {
	tests := []struct {
		sev  alert.Severity
		want bool
	}{
		{alert.SeverityInfo, true},
		{alert.SeverityLow, true},
		{alert.SeverityMedium, true},
		{alert.SeverityHigh, true},
		{alert.SeverityCritical, true},
		{alert.Severity(""), false},
		{alert.Severity("urgent"), false},
	}
	for _, tt := range tests {
		if got := tt.sev.Valid(); got != tt.want {
			t.Errorf("Severity(%q).Valid() = %v, want %v", tt.sev, got, tt.want)
		}
	}
}

func TestNewEnvelopeCarriesCurrentVersion(t *testing.T) {
	a := alert.New(sampleResult(), alert.SeverityCritical)

	env := alert.NewEnvelope(a)

	if env.Version != alert.PayloadVersion {
		t.Errorf("Version = %q, want %q", env.Version, alert.PayloadVersion)
	}
	if env.Alert.ID != a.ID {
		t.Errorf("Envelope.Alert.ID = %q, want %q", env.Alert.ID, a.ID)
	}
}
