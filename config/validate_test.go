package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Trustvian/trustvian/config"
)

func minimalValidConfig() config.PolicyConfig {
	return config.PolicyConfig{
		Version:         config.SchemaVersionV1,
		DefaultDecision: "observe_only",
		DefaultReason:   "no policy rules configured; observing by default",
	}
}

func fullValidConfig() config.PolicyConfig {
	return config.PolicyConfig{
		Version:         config.SchemaVersionV1,
		DefaultDecision: "observe_only",
		DefaultReason:   "no policy rules configured; observing by default",
		Rules: []config.PolicyRule{
			{
				Name: "block-secret-access-by-agent",
				When: config.PolicyCondition{
					ActorType:         "ai_agent",
					OperationCategory: "tool",
					Attributes:        map[string]string{"tool.category": "secrets"},
				},
				Unless: &config.PolicyCondition{
					Attributes: map[string]string{"approval": "human"},
				},
				Decision: "block",
				Reason:   "AI agent secret access requires human approval",
			},
			{
				Name: "critical-risk",
				When: config.PolicyCondition{
					MinRiskLevel: "critical",
				},
				Decision: "require_approval",
				Reason:   "critical risk requires manual approval",
			},
		},
	}
}

func TestValidateMinimalConfig(t *testing.T) {
	if err := minimalValidConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateFullConfig(t *testing.T) {
	if err := fullValidConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsMissingVersion(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Version = ""
	err := cfg.Validate()
	if !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrUnsupportedVersion)
	}
}

func TestValidateRejectsUnsupportedVersion(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Version = "v99"
	err := cfg.Validate()
	if !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrUnsupportedVersion)
	}
}

func TestValidateRejectsMissingDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.PolicyConfig
	}{
		{"empty decision", config.PolicyConfig{Version: config.SchemaVersionV1, DefaultReason: "x"}},
		{"empty reason", config.PolicyConfig{Version: config.SchemaVersionV1, DefaultDecision: "allow"}},
		{"both empty", config.PolicyConfig{Version: config.SchemaVersionV1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if !errors.Is(err, config.ErrMissingDefault) {
				t.Errorf("Validate() = %v, want %v", err, config.ErrMissingDefault)
			}
		})
	}
}

func TestValidateRejectsInvalidDefaultDecision(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.DefaultDecision = "blok" // typo for "block"
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidDecision) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidDecision)
	}
	if !strings.Contains(err.Error(), "default_decision") {
		t.Errorf("error %q does not identify the offending field", err)
	}
}

func TestValidateRejectsInvalidRuleDecision(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{Name: "r1", Decision: "blok", Reason: "x"}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidDecision) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidDecision)
	}
	if !strings.Contains(err.Error(), "rules[0].decision") {
		t.Errorf("error %q does not identify the offending rule/field", err)
	}
}

func TestValidateRejectsEmptyRuleName(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{Decision: "allow", Reason: "x"}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrEmptyRuleName) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrEmptyRuleName)
	}
}

func TestValidateRejectsMissingReason(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{Name: "r1", Decision: "allow"}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrMissingReason) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrMissingReason)
	}
}

func TestValidateRejectsDuplicateRuleName(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{
		{Name: "dup", Decision: "allow", Reason: "first"},
		{Name: "dup", Decision: "block", Reason: "second"},
	}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrDuplicateRuleName) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrDuplicateRuleName)
	}
}

func TestValidateRejectsInvalidActorType(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		When:     config.PolicyCondition{ActorType: "srevice"}, // typo
		Decision: "allow",
		Reason:   "x",
	}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidActorType) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidActorType)
	}
}

func TestValidateRejectsInvalidOperationCategory(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		When:     config.PolicyCondition{OperationCategory: "htpp"}, // typo
		Decision: "allow",
		Reason:   "x",
	}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidCategory) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidCategory)
	}
}

func TestValidateRejectsInvalidRiskLevel(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		When:     config.PolicyCondition{MinRiskLevel: "extreme"}, // not a real level
		Decision: "allow",
		Reason:   "x",
	}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidRiskLevel) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidRiskLevel)
	}
}

// TestValidateAcceptsEveryApprovalStatus is task 030's config
// validation coverage: every recognized event.ApprovalStatus value
// must round-trip through Validate without error, following the same
// pattern TestValidateRejectsInvalidActorType/OperationCategory
// already establish for their own enums.
func TestValidateAcceptsEveryApprovalStatus(t *testing.T) {
	for _, status := range []string{"not_required", "required", "approved", "denied"} {
		t.Run(status, func(t *testing.T) {
			cfg := minimalValidConfig()
			cfg.Rules = []config.PolicyRule{{
				Name:     "r1",
				When:     config.PolicyCondition{ApprovalStatus: status},
				Decision: "allow",
				Reason:   "x",
			}}
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestValidateRejectsInvalidApprovalStatus(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		When:     config.PolicyCondition{ApprovalStatus: "aproved"}, // typo
		Decision: "allow",
		Reason:   "x",
	}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidApprovalStatus) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidApprovalStatus)
	}
}

// TestValidateRejectsUnknownApprovalStatusDoesNotSilentlyMapToApproved
// is task 030's mandatory security regression (§20 of the task
// brief): an unrecognized ApprovalStatus string must be rejected by
// Validate, never silently accepted (and therefore never silently
// treated as any particular status, "approved" least of all) — an
// arbitrary-string-to-Approved mapping would be a real security
// defect, not a validation nicety.
func TestValidateRejectsUnknownApprovalStatusDoesNotSilentlyMapToApproved(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		When:     config.PolicyCondition{ApprovalStatus: "yes-approved-i-promise"},
		Decision: "allow",
		Reason:   "x",
	}}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("Validate() = nil, want an error — an unrecognized approval_status must never be silently accepted")
	}
}

func TestValidateAcceptsUnsetApprovalStatusAsDontCare(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		When:     config.PolicyCondition{}, // ApprovalStatus unset
		Decision: "allow",
		Reason:   "x",
	}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsInvalidUnlessCondition(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "r1",
		Decision: "allow",
		Reason:   "x",
		Unless:   &config.PolicyCondition{ActorType: "not-a-real-type"},
	}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidActorType) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrInvalidActorType)
	}
	if !strings.Contains(err.Error(), "unless") {
		t.Errorf("error %q does not identify the Unless condition as the source", err)
	}
}

func TestValidateAcceptsZeroValueCondition(t *testing.T) {
	// A condition with every field zero matches everything — legitimate
	// catch-all semantics inherited from policy.Condition, not a mistake
	// to reject.
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     "catch-all",
		When:     config.PolicyCondition{},
		Decision: "alert",
		Reason:   "flag everything for review",
	}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil (zero-value Condition is legitimate)", err)
	}
}

func TestValidateRejectsTooManyRules(t *testing.T) {
	cfg := minimalValidConfig()
	rules := make([]config.PolicyRule, 1001)
	for i := range rules {
		rules[i] = config.PolicyRule{Name: string(rune('a' + i%26)), Decision: "allow", Reason: "x"}
	}
	cfg.Rules = rules
	err := cfg.Validate()
	if !errors.Is(err, config.ErrTooManyRules) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrTooManyRules)
	}
}

func TestValidateRejectsOverlongName(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{{
		Name:     strings.Repeat("x", 257),
		Decision: "allow",
		Reason:   "x",
	}}
	err := cfg.Validate()
	if !errors.Is(err, config.ErrNameTooLong) {
		t.Errorf("Validate() = %v, want %v", err, config.ErrNameTooLong)
	}
}
