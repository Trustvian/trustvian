package config_test

import (
	"errors"
	"math"
	"testing"

	"github.com/Trustvian/trustvian/config"
)

func minimalValidAlertConfig() config.AlertConfig {
	return config.AlertConfig{
		Version: config.AlertSchemaVersionV1,
	}
}

func fullValidAlertConfig() config.AlertConfig {
	maxTrust := 0.3
	return config.AlertConfig{
		Version: config.AlertSchemaVersionV1,
		Rules: []config.AlertRuleConfig{
			{
				Name: "critical-block",
				When: config.AlertConditionConfig{
					Decision:        "block",
					MinRiskLevel:    "critical",
					ActorType:       "ai_agent",
					TargetCategory:  "database",
					MinAnomalyScore: 0.8,
					MaxTrustScore:   &maxTrust,
				},
				Severity: "critical",
			},
			{
				Name: "elevated-risk",
				When: config.AlertConditionConfig{
					MinRiskLevel: "high",
				},
				Severity: "high",
			},
		},
	}
}

func TestValidateMinimalAlertConfig(t *testing.T) {
	if err := minimalValidAlertConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateFullAlertConfig(t *testing.T) {
	if err := fullValidAlertConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateAlertConfigRejectsUnsupportedVersion(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Version = "v2"

	err := cfg.Validate()
	if !errors.Is(err, config.ErrUnsupportedAlertVersion) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrUnsupportedAlertVersion)
	}
}

func TestValidateAlertConfigAcceptsEmptyRules(t *testing.T) {
	// Unlike PolicyConfig, AlertConfig has no default/reason to
	// require — an empty Rules list is a valid, if inert, AlertConfig,
	// mirroring alert.Evaluate's own "no match means no alert" design.
	cfg := config.AlertConfig{Version: config.AlertSchemaVersionV1}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil for an empty Rules list", err)
	}
}

func TestValidateAlertConfigRejectsEmptyRuleName(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{Severity: "high"}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrEmptyRuleName) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrEmptyRuleName)
	}
}

func TestValidateAlertConfigRejectsDuplicateRuleName(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{
		{Name: "dup", Severity: "high"},
		{Name: "dup", Severity: "low"},
	}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrDuplicateRuleName) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrDuplicateRuleName)
	}
}

func TestValidateAlertConfigRejectsInvalidSeverity(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{Name: "r1", Severity: "urgent"}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidSeverity) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidSeverity)
	}
}

func TestValidateAlertConfigRejectsInvalidDecision(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{
		Name:     "r1",
		When:     config.AlertConditionConfig{Decision: "deny"},
		Severity: "high",
	}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidDecision) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidDecision)
	}
}

func TestValidateAlertConfigRejectsInvalidRiskLevel(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{
		Name:     "r1",
		When:     config.AlertConditionConfig{MinRiskLevel: "extreme"},
		Severity: "high",
	}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidRiskLevel) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidRiskLevel)
	}
}

func TestValidateAlertConfigRejectsInvalidActorType(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{
		Name:     "r1",
		When:     config.AlertConditionConfig{ActorType: "robot"},
		Severity: "high",
	}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidActorType) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidActorType)
	}
}

func TestValidateAlertConfigRejectsInvalidTargetCategory(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{
		Name:     "r1",
		When:     config.AlertConditionConfig{TargetCategory: "cloud"},
		Severity: "high",
	}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidTargetCategory) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidTargetCategory)
	}
}

func TestValidateAlertConfigRejectsInvalidMinAnomalyScore(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := minimalValidAlertConfig()
			cfg.Rules = []config.AlertRuleConfig{{
				Name:     "r1",
				When:     config.AlertConditionConfig{MinAnomalyScore: tt.value},
				Severity: "high",
			}}

			err := cfg.Validate()
			if !errors.Is(err, config.ErrInvalidThreshold) {
				t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidThreshold)
			}
		})
	}
}

func TestValidateAlertConfigRejectsInvalidMaxTrustScore(t *testing.T) {
	bad := 2.0
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{{
		Name:     "r1",
		When:     config.AlertConditionConfig{MaxTrustScore: &bad},
		Severity: "high",
	}}

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidThreshold) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidThreshold)
	}
}

func TestValidateAlertConfigRejectsTooManyRules(t *testing.T) {
	cfg := minimalValidAlertConfig()
	rules := make([]config.AlertRuleConfig, 1001)
	for i := range rules {
		rules[i] = config.AlertRuleConfig{Name: "r", Severity: "low"}
	}
	cfg.Rules = rules

	err := cfg.Validate()
	if !errors.Is(err, config.ErrTooManyRules) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrTooManyRules)
	}
}
