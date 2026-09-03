package policy_test

import (
	"testing"

	"github.com/Trustvian/trustvian/internal/trust"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/policy"
)

func benchPolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{Name: "block-high-risk", When: policy.Condition{MinRiskLevel: trust.RiskHigh}, Action: policy.DecisionBlock, Reason: "risk too high"},
			{Name: "alert-medium-risk", When: policy.Condition{MinRiskLevel: trust.RiskMedium}, Action: policy.DecisionAlert, Reason: "elevated risk"},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "risk within tolerance",
	}
}

// BenchmarkEvaluateMatch is the case where an early rule fires.
func BenchmarkEvaluateMatch(b *testing.B) {
	p := benchPolicy()
	in := input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskHigh)

	b.ReportAllocs()
	for b.Loop() {
		_ = p.Evaluate(in)
	}
}

// BenchmarkEvaluateDefault is the case where no rule matches and the
// policy falls through to its default — the common "everything normal"
// path.
func BenchmarkEvaluateDefault(b *testing.B) {
	p := benchPolicy()
	in := input(event.ActorTypeService, event.OperationCategoryHTTP, "payment-db", "production", trust.RiskLow)

	b.ReportAllocs()
	for b.Loop() {
		_ = p.Evaluate(in)
	}
}
