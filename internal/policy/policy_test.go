package policy_test

import (
	"strings"
	"testing"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

func input(actorType event.ActorType, category event.OperationCategory, target, env string, risk trust.RiskLevel) policy.Input {
	return policy.Input{
		Stable: features.StableFeatures{
			ActorType:         actorType,
			OperationCategory: category,
			OperationName:     "op",
			TargetName:        target,
			Environment:       env,
		},
		Trust: trust.Trust{Risk: risk},
	}
}

func TestDecisionValid(t *testing.T) {
	valid := []policy.Decision{
		policy.DecisionAllow, policy.DecisionObserveOnly, policy.DecisionAlert,
		policy.DecisionChallenge, policy.DecisionRequireApproval, policy.DecisionBlock,
	}
	for _, d := range valid {
		if !d.Valid() {
			t.Errorf("%q.Valid() = false, want true", d)
		}
	}

	invalid := []policy.Decision{"", "ALLOW", "allowed", "bogus"}
	for _, d := range invalid {
		if d.Valid() {
			t.Errorf("%q.Valid() = true, want false", d)
		}
	}
}

func TestConditionMatchesEmptyMatchesEverything(t *testing.T) {
	c := policy.Condition{}
	in := input(event.ActorTypeAIAgent, event.OperationCategoryTool, "secrets-manager", "production", trust.RiskCritical)

	if !c.Matches(in) {
		t.Fatalf("empty Condition should match every Input")
	}
}

func TestConditionMatchesPerField(t *testing.T) {
	base := input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskMedium)

	tests := []struct {
		name string
		cond policy.Condition
		want bool
	}{
		{"actor type matches", policy.Condition{ActorType: event.ActorTypeService}, true},
		{"actor type mismatches", policy.Condition{ActorType: event.ActorTypeAIAgent}, false},
		{"category matches", policy.Condition{OperationCategory: event.OperationCategoryHTTP}, true},
		{"category mismatches", policy.Condition{OperationCategory: event.OperationCategoryDB}, false},
		{"target matches", policy.Condition{TargetName: "payment-db"}, true},
		{"target mismatches", policy.Condition{TargetName: "secrets-manager"}, false},
		{"environment matches", policy.Condition{Environment: "production"}, true},
		{"environment mismatches", policy.Condition{Environment: "staging"}, false},
		{"min risk satisfied exactly", policy.Condition{MinRiskLevel: trust.RiskMedium}, true},
		{"min risk satisfied by higher", policy.Condition{MinRiskLevel: trust.RiskLow}, true},
		{"min risk not satisfied", policy.Condition{MinRiskLevel: trust.RiskHigh}, false},
		{"all fields match", policy.Condition{
			ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryHTTP,
			TargetName: "payment-db", Environment: "production", MinRiskLevel: trust.RiskMedium,
		}, true},
		{"one field mismatches among many", policy.Condition{
			ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryHTTP,
			TargetName: "admin-db", Environment: "production", MinRiskLevel: trust.RiskMedium,
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cond.Matches(base); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateRuleOrderingFirstMatchWins(t *testing.T) {
	p := policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "specific-block",
				When:   policy.Condition{TargetName: "secrets-manager"},
				Action: policy.DecisionBlock,
				Reason: "secrets access is always blocked",
			},
			{
				Name:   "catch-all-observe",
				When:   policy.Condition{}, // matches everything
				Action: policy.DecisionObserveOnly,
				Reason: "default observation rule",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "unreachable in this test",
	}

	in := input(event.ActorTypeAIAgent, event.OperationCategoryTool, "secrets-manager", "production", trust.RiskHigh)
	got := p.Evaluate(in)

	if got.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q, want %q (the first matching rule)", got.Decision, policy.DecisionBlock)
	}
	if got.Explanation.RuleName != "specific-block" {
		t.Fatalf("RuleName = %q, want %q", got.Explanation.RuleName, "specific-block")
	}

	// A different input that only matches the catch-all should get the
	// second rule, proving the first rule's specificity, not just
	// "always the first entry", drives the result.
	in2 := input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow)
	got2 := p.Evaluate(in2)
	if got2.Decision != policy.DecisionObserveOnly {
		t.Fatalf("Decision = %q, want %q", got2.Decision, policy.DecisionObserveOnly)
	}
}

func TestEvaluateDefaultFallback(t *testing.T) {
	p := policy.Policy{
		Rules: []policy.Rule{
			{Name: "block-secrets", When: policy.Condition{TargetName: "secrets-manager"}, Action: policy.DecisionBlock, Reason: "secrets access is always blocked"},
		},
		DefaultAction: policy.DecisionObserveOnly,
		DefaultReason: "no matching rule; observe by default",
	}

	in := input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow)
	got := p.Evaluate(in)

	if got.Decision != policy.DecisionObserveOnly {
		t.Fatalf("Decision = %q, want %q", got.Decision, policy.DecisionObserveOnly)
	}
	if !got.Explanation.MatchedDefault {
		t.Fatalf("MatchedDefault = false, want true")
	}
	if got.Explanation.Reason != "no matching rule; observe by default" {
		t.Fatalf("Reason = %q, want the configured DefaultReason", got.Explanation.Reason)
	}
}

func TestEvaluateUnlessOverride(t *testing.T) {
	p := policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "alert-external-calls",
				When:   policy.Condition{OperationCategory: event.OperationCategoryExternal},
				Unless: &policy.Condition{TargetName: "partner-api"},
				Action: policy.DecisionAlert,
				Reason: "external calls are unexpected",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "no matching rule",
	}

	// Unless does NOT match: rule fires.
	blocked := input(event.ActorTypeService, event.OperationCategoryExternal, "unknown-host", "production", trust.RiskLow)
	got := p.Evaluate(blocked)
	if got.Decision != policy.DecisionAlert {
		t.Fatalf("Decision = %q, want %q when Unless does not match", got.Decision, policy.DecisionAlert)
	}

	// Unless DOES match: rule is suppressed, falls through to default.
	excepted := input(event.ActorTypeService, event.OperationCategoryExternal, "partner-api", "production", trust.RiskLow)
	got2 := p.Evaluate(excepted)
	if got2.Decision != policy.DecisionAllow {
		t.Fatalf("Decision = %q, want %q when Unless matches (rule suppressed)", got2.Decision, policy.DecisionAllow)
	}
	if !got2.Explanation.MatchedDefault {
		t.Fatalf("MatchedDefault = false, want true when the only rule was suppressed by Unless")
	}
}

func TestEvaluateFailsClosedOnZeroValuePolicy(t *testing.T) {
	var p policy.Policy // zero value: no rules, no default configured

	got := p.Evaluate(input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow))

	if got.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q for an unconfigured Policy, want %q (fail closed)", got.Decision, policy.DecisionBlock)
	}
	if got.Explanation.Reason == "" {
		t.Fatalf("Explanation.Reason is empty for a fail-closed Decision")
	}
}

func TestEvaluateFailsClosedOnInvalidDefaultAction(t *testing.T) {
	p := policy.Policy{DefaultAction: "not-a-real-decision", DefaultReason: "set but invalid"}

	got := p.Evaluate(input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow))

	if got.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q for an invalid DefaultAction, want %q (fail closed)", got.Decision, policy.DecisionBlock)
	}
}

func TestEvaluateFailsClosedOnEmptyDefaultReason(t *testing.T) {
	p := policy.Policy{DefaultAction: policy.DecisionAllow, DefaultReason: ""}

	got := p.Evaluate(input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow))

	if got.Decision != policy.DecisionBlock {
		t.Fatalf("Decision = %q when DefaultReason is empty, want %q (fail closed, never a silent ALLOW)", got.Decision, policy.DecisionBlock)
	}
}

func TestEvaluateAlwaysProducesNonEmptyExplanationReason(t *testing.T) {
	policies := []policy.Policy{
		{},
		{DefaultAction: policy.DecisionAllow, DefaultReason: "explicit default"},
		{
			Rules:         []policy.Rule{{Name: "r1", When: policy.Condition{}, Action: policy.DecisionAlert, Reason: "always alert"}},
			DefaultAction: policy.DecisionBlock,
			DefaultReason: "unreachable",
		},
	}
	in := input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow)

	for i, p := range policies {
		got := p.Evaluate(in)
		if got.Explanation.Reason == "" {
			t.Errorf("policies[%d]: Explanation.Reason is empty", i)
		}
		if !got.Decision.Valid() {
			t.Errorf("policies[%d]: Decision %q is not a valid Decision", i, got.Decision)
		}
	}
}

func TestResultString(t *testing.T) {
	matched := policy.Result{Decision: policy.DecisionBlock, Explanation: policy.Explanation{RuleName: "block-secrets", Reason: "secrets access is always blocked"}}
	if s := matched.String(); !strings.Contains(s, "block-secrets") || !strings.Contains(s, "block") {
		t.Fatalf("String() = %q, want it to mention the rule name and decision", s)
	}

	defaulted := policy.Result{Decision: policy.DecisionAllow, Explanation: policy.Explanation{Reason: "no matching rule", MatchedDefault: true}}
	if s := defaulted.String(); !strings.Contains(s, "default") {
		t.Fatalf("String() = %q, want it to mention that the default applied", s)
	}
}
