package main

import (
	"fmt"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
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

// newEngine constructs the Engine every subcommand analyzes/observes
// against. With no configPath, it keeps the CLI's existing built-in
// defaultPolicy unchanged — this is the hard compatibility requirement
// task 021 exists to preserve, not just an implementation detail.
//
// With configPath set, it loads and compiles a real Policy from that
// file via the same public config package (config.LoadFile,
// config.CompilePolicy) any other caller outside this module would
// use — this function does not read internal/policy itself for that
// path, and does not re-implement any parsing or validation task 020
// already owns. A load or compile failure here is returned, not
// swallowed: the caller must fail the whole command rather than fall
// back to defaultPolicy, per this task's central security invariant
// (an explicitly requested config that cannot be safely loaded and
// compiled must never result in analysis continuing under a different
// policy).
func newEngine(configPath string) (*trustvian.Engine, error) {
	if configPath == "" {
		return trustvian.NewEngine(trustvian.WithPolicy(defaultPolicy())), nil
	}

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	p, err := config.CompilePolicy(cfg)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", configPath, err)
	}
	return trustvian.NewEngine(trustvian.WithPolicy(p)), nil
}
