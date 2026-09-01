package trust_test

import (
	"testing"

	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/trust"
)

func almostEqual(a, b float64) bool {
	const epsilon = 1e-9
	diff := a - b
	return diff < epsilon && diff > -epsilon
}

func TestComputeMultiplicativeFormula(t *testing.T) {
	tests := []struct {
		name               string
		anomalyScore       float64
		anomalyConfidence  float64
		identityConfidence float64
		contextRisk        float64
	}{
		{"all zero risk", 0, 1, 1, 0},
		{"moderate anomaly full confidence", 0.4, 1, 0.9, 0.1},
		{"high anomaly full confidence", 0.91, 1, 0.95, 0.63},
		{"low identity", 0.2, 1, 0.3, 0},
		{"high context risk", 0, 1, 1, 0.8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			an := anomaly.Anomaly{Score: tt.anomalyScore, Confidence: tt.anomalyConfidence}
			got := trust.Compute(an, tt.identityConfidence, tt.contextRisk, trust.DefaultConfig())

			effectiveAnomaly := tt.anomalyScore * tt.anomalyConfidence
			want := tt.identityConfidence * (1 - effectiveAnomaly) * (1 - tt.contextRisk)

			if !almostEqual(got.Score, want) {
				t.Fatalf("Score = %v, want %v (documented formula)", got.Score, want)
			}
		})
	}
}

func TestComputeColdStartDoesNotTankTrust(t *testing.T) {
	// A brand-new fingerprint: anomaly.Score is near-max but Confidence
	// is 0. Trust must fall back to identity/context alone, not be
	// dragged down by an unreliable anomaly reading.
	an := anomaly.Anomaly{Score: 0.97, Confidence: 0}

	got := trust.Compute(an, 0.9, 0.1, trust.DefaultConfig())

	want := 0.9 * 1.0 * 0.9 // identity * (1 - 0) * (1 - context)
	if !almostEqual(got.Score, want) {
		t.Fatalf("Score = %v, want %v (cold start should not be penalized by an unreliable anomaly score)", got.Score, want)
	}
}

func TestComputePartialConfidenceScalesAnomalyImpact(t *testing.T) {
	an := anomaly.Anomaly{Score: 1.0, Confidence: 0.5}

	got := trust.Compute(an, 1.0, 0, trust.DefaultConfig())

	want := 1.0 * (1 - 0.5) * 1.0
	if !almostEqual(got.Score, want) {
		t.Fatalf("Score = %v, want %v (effective anomaly should be Score*Confidence = 0.5)", got.Score, want)
	}
}

func TestComputeSevereConfidentAnomalyDominatesTrust(t *testing.T) {
	an := anomaly.Anomaly{Score: 1.0, Confidence: 1.0}

	got := trust.Compute(an, 1.0, 0, trust.DefaultConfig())

	if got.Score != 0 {
		t.Fatalf("Score = %v, want 0 for a maximal, fully-confident anomaly regardless of perfect identity/context", got.Score)
	}
}

func TestComputeRiskLevelThresholds(t *testing.T) {
	cfg := trust.DefaultConfig() // Medium 0.25, High 0.5, Critical 0.75

	tests := []struct {
		name       string
		trustScore float64 // achieved via identityConfidence with zero anomaly/context
		wantRisk   trust.RiskLevel
	}{
		{"residual just below medium", 0.7501, trust.RiskLow},    // residual 0.2499
		{"residual exactly at medium", 0.75, trust.RiskMedium},   // residual 0.25
		{"residual just below high", 0.5001, trust.RiskMedium},   // residual 0.4999
		{"residual exactly at high", 0.5, trust.RiskHigh},        // residual 0.5
		{"residual just below critical", 0.2501, trust.RiskHigh}, // residual 0.7499
		{"residual exactly at critical", 0.25, trust.RiskCritical},
		{"zero trust", 0, trust.RiskCritical},
		{"full trust", 1, trust.RiskLow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			an := anomaly.Anomaly{Score: 0, Confidence: 1}
			got := trust.Compute(an, tt.trustScore, 0, cfg)
			if got.Risk != tt.wantRisk {
				t.Fatalf("identity=%v -> Score=%v Risk = %v, want %v", tt.trustScore, got.Score, got.Risk, tt.wantRisk)
			}
		})
	}
}

func TestComputeClampsOutOfRangeInputs(t *testing.T) {
	tests := []struct {
		name               string
		anomalyScore       float64
		identityConfidence float64
		contextRisk        float64
	}{
		{"negative identity", 0, -0.5, 0},
		{"identity above one", 0, 1.5, 0},
		{"negative context risk", 0, 1, -0.3},
		{"context risk above one", 0, 1, 2.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			an := anomaly.Anomaly{Score: tt.anomalyScore, Confidence: 1}
			got := trust.Compute(an, tt.identityConfidence, tt.contextRisk, trust.DefaultConfig())

			if got.Score < 0 || got.Score > 1 {
				t.Fatalf("Score = %v, want a value clamped to [0,1]", got.Score)
			}
			if got.IdentityConfidence < 0 || got.IdentityConfidence > 1 {
				t.Fatalf("IdentityConfidence = %v, want clamped to [0,1]", got.IdentityConfidence)
			}
			if got.ContextRisk < 0 || got.ContextRisk > 1 {
				t.Fatalf("ContextRisk = %v, want clamped to [0,1]", got.ContextRisk)
			}
		})
	}
}

func TestComputeRetainsComponents(t *testing.T) {
	an := anomaly.Anomaly{Score: 0.42, Confidence: 0.77}

	got := trust.Compute(an, 0.88, 0.15, trust.DefaultConfig())

	if got.AnomalyScore != 0.42 {
		t.Fatalf("AnomalyScore = %v, want 0.42", got.AnomalyScore)
	}
	if got.AnomalyConfidence != 0.77 {
		t.Fatalf("AnomalyConfidence = %v, want 0.77", got.AnomalyConfidence)
	}
	if got.IdentityConfidence != 0.88 {
		t.Fatalf("IdentityConfidence = %v, want 0.88", got.IdentityConfidence)
	}
	if got.ContextRisk != 0.15 {
		t.Fatalf("ContextRisk = %v, want 0.15", got.ContextRisk)
	}
}

func TestRiskLevelAtLeast(t *testing.T) {
	tests := []struct {
		level trust.RiskLevel
		min   trust.RiskLevel
		want  bool
	}{
		{trust.RiskLow, trust.RiskLow, true},
		{trust.RiskLow, trust.RiskMedium, false},
		{trust.RiskHigh, trust.RiskMedium, true},
		{trust.RiskHigh, trust.RiskHigh, true},
		{trust.RiskHigh, trust.RiskCritical, false},
		{trust.RiskCritical, trust.RiskLow, true},
		{trust.RiskMedium, trust.RiskHigh, false},
	}

	for _, tt := range tests {
		if got := tt.level.AtLeast(tt.min); got != tt.want {
			t.Errorf("%s.AtLeast(%s) = %v, want %v", tt.level, tt.min, got, tt.want)
		}
	}
}
