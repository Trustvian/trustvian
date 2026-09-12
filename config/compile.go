package config

import (
	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

// CompilePolicy validates cfg and translates it into a policy.Policy —
// the exact type trustvian.WithPolicy accepts. It always validates
// cfg itself first (the same guard Validate performs, run
// unconditionally here too, so compiling directly is never less safe
// than validating first and compiling second).
//
// CompilePolicy is pure: no I/O, no global state, no randomness, no
// wall-clock dependency, and no mutation of cfg. The same cfg value
// always compiles to an identical policy.Policy — see
// TestCompilePolicyIsDeterministic.
//
// The returned policy.Policy is safe for a caller outside this module
// to receive and pass straight into trustvian.WithPolicy via type
// inference (e.g. `engine := trustvian.NewEngine(trustvian.WithPolicy(p))`
// where p was obtained from this function) without importing
// internal/policy — see ADR 0008 for why this works and why it is the
// deliberate design, not an accident of Go's type system.
func CompilePolicy(cfg PolicyConfig) (policy.Policy, error) {
	if err := cfg.Validate(); err != nil {
		return policy.Policy{}, err
	}

	rules := make([]policy.Rule, len(cfg.Rules))
	for i, r := range cfg.Rules {
		compiled := policy.Rule{
			Name:   r.Name,
			When:   compileCondition(r.When),
			Action: policy.Decision(r.Decision),
			Reason: r.Reason,
		}
		if r.Unless != nil {
			unless := compileCondition(*r.Unless)
			compiled.Unless = &unless
		}
		rules[i] = compiled
	}

	return policy.Policy{
		Rules:         rules,
		DefaultAction: policy.Decision(cfg.DefaultDecision),
		DefaultReason: cfg.DefaultReason,
	}, nil
}

// compileCondition is a direct field-for-field translation — it
// contains no matching logic of its own. policy.Condition.Matches
// remains the sole place matching is evaluated; this function only
// ever produces the policy.Condition value Matches will later read.
func compileCondition(c PolicyCondition) policy.Condition {
	compiled := policy.Condition{
		TargetName:  c.TargetName,
		Environment: c.Environment,
	}
	if c.ActorType != "" {
		compiled.ActorType = event.ActorType(c.ActorType)
	}
	if c.OperationCategory != "" {
		compiled.OperationCategory = event.OperationCategory(c.OperationCategory)
	}
	if c.MinRiskLevel != "" {
		compiled.MinRiskLevel = trust.RiskLevel(c.MinRiskLevel)
	}
	if len(c.Attributes) > 0 {
		compiled.Attributes = c.Attributes
	}
	if c.ApprovalStatus != "" {
		compiled.ApprovalStatus = event.ApprovalStatus(c.ApprovalStatus)
	}
	return compiled
}

// CompileAlerts validates cfg and translates it into a []alert.Rule —
// the exact type alert.Evaluate accepts. Like CompilePolicy, it always
// validates cfg itself first, and is pure: no I/O, no global state, no
// wall-clock dependency, no mutation of cfg, and no coupling to Policy
// at all — a genuinely separate compilation from CompilePolicy's,
// matching the Policy/Alert separation config/alert.go's package
// comment and ADR 0009 document. CompileAlerts contains no matching
// logic of its own; alert.Condition.Matches remains the sole place
// Alert matching is evaluated, exactly as CompilePolicy leaves
// policy.Condition.Matches as Policy's sole evaluator.
func CompileAlerts(cfg AlertConfig) ([]alert.Rule, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	rules := make([]alert.Rule, len(cfg.Rules))
	for i, r := range cfg.Rules {
		rules[i] = alert.Rule{
			Name:     r.Name,
			When:     compileAlertCondition(r.When),
			Severity: alert.Severity(r.Severity),
		}
	}
	return rules, nil
}

// compileAlertCondition is a direct field-for-field translation, the
// AlertCondition analogue of compileCondition above — it contains no
// matching logic of its own.
func compileAlertCondition(c AlertConditionConfig) alert.Condition {
	compiled := alert.Condition{
		MinAnomalyScore: c.MinAnomalyScore,
		MaxTrustScore:   c.MaxTrustScore,
	}
	if c.Decision != "" {
		compiled.Decision = policy.Decision(c.Decision)
	}
	if c.MinRiskLevel != "" {
		compiled.MinRiskLevel = trust.RiskLevel(c.MinRiskLevel)
	}
	if c.ActorType != "" {
		compiled.ActorType = event.ActorType(c.ActorType)
	}
	if c.TargetCategory != "" {
		compiled.TargetCategory = event.TargetCategory(c.TargetCategory)
	}
	return compiled
}
