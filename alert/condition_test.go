package alert_test

import (
	"reflect"
	"testing"

	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

func TestConditionZeroValueMatchesEverything(t *testing.T) {
	result := sampleResult()

	if !(alert.Condition{}).Matches(result) {
		t.Error("zero-value Condition did not match a result; every unset field should mean \"don't care\"")
	}
}

func TestConditionMatches(t *testing.T) {
	maxTrust := func(v float64) *float64 { return &v }

	tests := []struct {
		name string
		cond alert.Condition
		want bool
	}{
		{"decision matches", alert.Condition{Decision: policy.DecisionBlock}, true},
		{"decision mismatches", alert.Condition{Decision: policy.DecisionAllow}, false},
		{"risk floor met", alert.Condition{MinRiskLevel: trust.RiskMedium}, true},
		{"risk floor exactly met", alert.Condition{MinRiskLevel: trust.RiskHigh}, true},
		{"risk floor not met", alert.Condition{MinRiskLevel: trust.RiskCritical}, false},
		{"actor type matches", alert.Condition{ActorType: event.ActorTypeService}, true},
		{"actor type mismatches", alert.Condition{ActorType: event.ActorTypeUser}, false},
		{"target category matches", alert.Condition{TargetCategory: event.TargetCategoryDatabase}, true},
		{"target category mismatches", alert.Condition{TargetCategory: event.TargetCategoryExternal}, false},
		{"anomaly floor met", alert.Condition{MinAnomalyScore: 0.5}, true},
		{"anomaly floor exactly met", alert.Condition{MinAnomalyScore: 0.9}, true},
		{"anomaly floor not met", alert.Condition{MinAnomalyScore: 0.95}, false},
		{"anomaly floor zero value never excludes", alert.Condition{MinAnomalyScore: 0}, true},
		{"trust ceiling met", alert.Condition{MaxTrustScore: maxTrust(0.5)}, true},
		{"trust ceiling exactly met", alert.Condition{MaxTrustScore: maxTrust(0.2)}, true},
		{"trust ceiling not met", alert.Condition{MaxTrustScore: maxTrust(0.1)}, false},
		{"trust ceiling nil never excludes", alert.Condition{MaxTrustScore: nil}, true},
		{
			"every field set and satisfied",
			alert.Condition{
				Decision:        policy.DecisionBlock,
				MinRiskLevel:    trust.RiskHigh,
				ActorType:       event.ActorTypeService,
				TargetCategory:  event.TargetCategoryDatabase,
				MinAnomalyScore: 0.9,
				MaxTrustScore:   maxTrust(0.2),
			},
			true,
		},
		{
			"every field set, one violated",
			alert.Condition{
				Decision:        policy.DecisionBlock,
				MinRiskLevel:    trust.RiskHigh,
				ActorType:       event.ActorTypeService,
				TargetCategory:  event.TargetCategoryExternal, // violated
				MinAnomalyScore: 0.9,
				MaxTrustScore:   maxTrust(0.2),
			},
			false,
		},
	}

	result := sampleResult()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cond.Matches(result); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateDecisionDoesNotAutomaticallyImplyAlert(t *testing.T) {
	result := sampleResult() // Decision: BLOCK, high risk, high anomaly

	_, matched := alert.Evaluate(result, nil)

	if matched {
		t.Error("Evaluate matched with no configured rules; a Decision must never automatically imply an Alert")
	}
}

func TestEvaluateMatchingRuleProducesAlert(t *testing.T) {
	result := sampleResult()
	rules := []alert.Rule{
		{Name: "critical-block", When: alert.Condition{Decision: policy.DecisionBlock}, Severity: alert.SeverityCritical},
	}

	a, matched := alert.Evaluate(result, rules)

	if !matched {
		t.Fatal("Evaluate did not match a rule that should have matched")
	}
	if a.Severity != alert.SeverityCritical {
		t.Errorf("Severity = %q, want %q (exactly what the matched rule configured)", a.Severity, alert.SeverityCritical)
	}
	if a.Decision != result.Decision {
		t.Errorf("Decision = %q, want %q", a.Decision, result.Decision)
	}
}

func TestEvaluateNonMatchingRuleProducesNoAlert(t *testing.T) {
	result := sampleResult() // Decision: BLOCK
	rules := []alert.Rule{
		{Name: "allow-only", When: alert.Condition{Decision: policy.DecisionAllow}, Severity: alert.SeverityInfo},
	}

	_, matched := alert.Evaluate(result, rules)

	if matched {
		t.Error("Evaluate matched a rule whose Condition does not match the Result")
	}
}

func TestEvaluateFirstMatchWins(t *testing.T) {
	result := sampleResult()
	rules := []alert.Rule{
		{Name: "first", When: alert.Condition{Decision: policy.DecisionBlock}, Severity: alert.SeverityLow},
		{Name: "second", When: alert.Condition{Decision: policy.DecisionBlock}, Severity: alert.SeverityCritical},
	}

	a, matched := alert.Evaluate(result, rules)

	if !matched {
		t.Fatal("expected a match")
	}
	if a.Severity != alert.SeverityLow {
		t.Errorf("Severity = %q, want %q (first matching rule must win)", a.Severity, alert.SeverityLow)
	}
}

func TestEvaluateDoesNotMutateResult(t *testing.T) {
	result := sampleResult()
	before := deepCopyResult(result)
	rules := []alert.Rule{{When: alert.Condition{Decision: policy.DecisionBlock}, Severity: alert.SeverityHigh}}

	_, _ = alert.Evaluate(result, rules)

	if !reflect.DeepEqual(result, before) {
		t.Errorf("Evaluate mutated its Result argument: got %+v, want %+v", result, before)
	}
}
