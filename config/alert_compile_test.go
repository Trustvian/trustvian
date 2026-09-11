package config_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/config"
	"github.com/Trustvian/trustvian/event"
)

func TestCompileAlertsRejectsInvalidConfig(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Version = "bogus"

	_, err := config.CompileAlerts(cfg)
	if !errors.Is(err, config.ErrUnsupportedAlertVersion) {
		t.Errorf("CompileAlerts() err = %v, want %v", err, config.ErrUnsupportedAlertVersion)
	}
}

// TestCompileAlertsPreservesRuleOrder proves alert.Evaluate's
// first-match-wins contract survives decoding/compiling unchanged —
// Rules is never routed through a map that could reorder it.
func TestCompileAlertsPreservesRuleOrder(t *testing.T) {
	cfg := minimalValidAlertConfig()
	cfg.Rules = []config.AlertRuleConfig{
		{Name: "first", Severity: "low"},
		{Name: "second", Severity: "medium"},
		{Name: "third", Severity: "high"},
	}

	rules, err := config.CompileAlerts(cfg)
	if err != nil {
		t.Fatalf("CompileAlerts: %v", err)
	}

	got := []string{rules[0].Name, rules[1].Name, rules[2].Name}
	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compiled rule order = %v, want %v", got, want)
	}
}

func TestCompileAlertsIsDeterministic(t *testing.T) {
	cfg := fullValidAlertConfig()

	r1, err := config.CompileAlerts(cfg)
	if err != nil {
		t.Fatalf("CompileAlerts (1st): %v", err)
	}
	r2, err := config.CompileAlerts(cfg)
	if err != nil {
		t.Fatalf("CompileAlerts (2nd): %v", err)
	}

	if !reflect.DeepEqual(r1, r2) {
		t.Errorf("compiling the same AlertConfig twice produced different results:\n%+v\nvs\n%+v", r1, r2)
	}
}

func TestCompileAlertsDoesNotMutateConfig(t *testing.T) {
	cfg := fullValidAlertConfig()
	before := fullValidAlertConfig()

	_, err := config.CompileAlerts(cfg)
	if err != nil {
		t.Fatalf("CompileAlerts: %v", err)
	}

	if !reflect.DeepEqual(cfg, before) {
		t.Errorf("CompileAlerts mutated its input:\ngot  %+v\nwant %+v", cfg, before)
	}
}

func TestCompileAlertConditionTranslatesEveryField(t *testing.T) {
	maxTrust := 0.4
	cfg := config.AlertConfig{
		Version: config.AlertSchemaVersionV1,
		Rules: []config.AlertRuleConfig{{
			Name: "r1",
			When: config.AlertConditionConfig{
				Decision:        "block",
				MinRiskLevel:    "high",
				ActorType:       "ai_agent",
				TargetCategory:  "database",
				MinAnomalyScore: 0.7,
				MaxTrustScore:   &maxTrust,
			},
			Severity: "critical",
		}},
	}

	rules, err := config.CompileAlerts(cfg)
	if err != nil {
		t.Fatalf("CompileAlerts: %v", err)
	}

	when := rules[0].When
	if string(when.Decision) != "block" {
		t.Errorf("Decision = %q, want %q", when.Decision, "block")
	}
	if string(when.MinRiskLevel) != "high" {
		t.Errorf("MinRiskLevel = %q, want %q", when.MinRiskLevel, "high")
	}
	if string(when.ActorType) != "ai_agent" {
		t.Errorf("ActorType = %q, want %q", when.ActorType, "ai_agent")
	}
	if string(when.TargetCategory) != "database" {
		t.Errorf("TargetCategory = %q, want %q", when.TargetCategory, "database")
	}
	if when.MinAnomalyScore != 0.7 {
		t.Errorf("MinAnomalyScore = %v, want %v", when.MinAnomalyScore, 0.7)
	}
	if when.MaxTrustScore == nil || *when.MaxTrustScore != 0.4 {
		t.Errorf("MaxTrustScore = %v, want pointer to %v", when.MaxTrustScore, 0.4)
	}
	if string(rules[0].Severity) != "critical" {
		t.Errorf("Severity = %q, want %q", rules[0].Severity, "critical")
	}
}

// TestEndToEndConfiguredAlertProducesExpectedAlert is this task's own
// central acceptance test: a declarative AlertConfig, compiled and run
// against a real Result from a real Engine.Analyze call, produces the
// Alert alert.Evaluate is documented to produce — not just that the
// config decodes or CompileAlerts returns no error. Mirrors
// TestEndToEndConfiguredPolicyProducesConfiguredDecision's shape, for
// the structurally independent Alert side of v0.5.
func TestEndToEndConfiguredAlertProducesExpectedAlert(t *testing.T) {
	cfg := config.AlertConfig{
		Version: config.AlertSchemaVersionV1,
		Rules: []config.AlertRuleConfig{{
			Name: "critical-risk",
			When: config.AlertConditionConfig{
				MinRiskLevel: "critical",
			},
			Severity: "critical",
		}},
	}

	rules, err := config.CompileAlerts(cfg)
	if err != nil {
		t.Fatalf("CompileAlerts: %v", err)
	}

	// A real Engine, using its own zero-configuration default Policy —
	// Alert configuration is deliberately independent of Policy
	// configuration (see config/alert.go's package comment), so this
	// test does not configure a custom Policy at all.
	engine := trustvian.NewEngine()
	ev := event.Event{
		ID:        "evt-1",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "svc-payment", Type: event.ActorTypeService, IdentityConfidence: 1.0},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /orders"},
		Target:    event.Target{Name: "orders-api"},
	}
	result, err := engine.Analyze(context.Background(), ev)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// Force the exact Result shape this test needs to exercise
	// alert.Evaluate deterministically, without depending on which
	// combination of anomaly/trust inputs actually reaches
	// RiskCritical (that mapping is internal/trust's own concern, with
	// its own tests) — only Result.Trust.Risk is read by
	// alert.Condition.Matches, so overriding it here keeps this test
	// focused on the config -> CompileAlerts -> alert.Evaluate path.
	result.Trust.Risk = "critical"

	got, matched := alert.Evaluate(result, rules)
	if !matched {
		t.Fatal("alert.Evaluate() matched = false, want true")
	}
	if got.Severity != alert.SeverityCritical {
		t.Errorf("Alert.Severity = %q, want %q", got.Severity, alert.SeverityCritical)
	}
	if got.FingerprintID != result.Fingerprint.ID {
		t.Errorf("Alert.FingerprintID = %q, want %q", got.FingerprintID, result.Fingerprint.ID)
	}

	// A Result that does not match must produce no Alert at all — the
	// documented alert.Evaluate asymmetry with policy.Policy.Evaluate
	// (no default, no fail-closed requirement).
	result.Trust.Risk = "low"
	_, matched = alert.Evaluate(result, rules)
	if matched {
		t.Error("alert.Evaluate() matched = true for a non-matching Result, want false")
	}
}
