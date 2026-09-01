package main

import (
	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

// defaultPolicy is the CLI's starter policy: block on high or critical
// risk, alert on medium, allow otherwise. It exists so `trustvian
// analyze` produces a differentiated, informative report out of the box.
// A real deployment embedding the Go SDK is expected to configure its
// own policy via trustvian.WithPolicy — this one is not meant to be a
// production default.
func defaultPolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "block-high-risk",
				When:   policy.Condition{MinRiskLevel: trust.RiskHigh},
				Action: policy.DecisionBlock,
				Reason: "trust score indicates high or critical risk",
			},
			{
				Name:   "alert-medium-risk",
				When:   policy.Condition{MinRiskLevel: trust.RiskMedium},
				Action: policy.DecisionAlert,
				Reason: "trust score indicates elevated risk",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "risk within tolerance",
	}
}

func newEngine() *trustvian.Engine {
	return trustvian.NewEngine(trustvian.WithPolicy(defaultPolicy()))
}
