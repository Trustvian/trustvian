package policy_test

import (
	"testing"

	"github.com/Trustvian/trustvian/internal/trust"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
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

// BenchmarkEvaluateApprovalCondition (task 030) is
// BenchmarkEvaluateMatch's approval-aware analogue: a Rule whose
// Unless matches ApprovalStatus is still a single additional equality
// comparison in Condition.Matches, not a new evaluation stage — this
// benchmark's B/op and allocs/op are expected to match
// BenchmarkEvaluateMatch's exactly, confirming approval evaluation adds
// no allocation of its own.
func BenchmarkEvaluateApprovalCondition(b *testing.B) {
	p := policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "shell-execute-requires-approval",
				When:   policy.Condition{OperationCategory: event.OperationCategoryTool, TargetName: "shell.execute"},
				Unless: &policy.Condition{ApprovalStatus: event.ApprovalApproved},
				Action: policy.DecisionBlock,
				Reason: "shell.execute requires approval",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "no approval requirement configured",
	}
	in := policy.Input{
		Stable: features.StableFeatures{
			ActorType:         event.ActorTypeAIAgent,
			OperationCategory: event.OperationCategoryTool,
			TargetName:        "shell.execute",
		},
		ApprovalStatus: event.ApprovalDenied,
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = p.Evaluate(in)
	}
}
