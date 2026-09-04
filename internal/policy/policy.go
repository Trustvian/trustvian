// Package policy is the final pipeline stage: it turns a Trust result and
// the event's stable features into a Decision, by evaluating a Policy — a
// data value, not code — against them.
//
// Evaluation is first-match-wins over an ordered Rule list, with a
// mandatory default for when nothing matches. Per CLAUDE.md's "never
// silently weaken a security policy", there is no implicit ALLOW: an
// unconfigured or invalid default fails closed to BLOCK rather than
// falling through to any particular Decision by accident.
package policy

import (
	"fmt"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/trust"
)

// Decision is the final verdict a Policy produces.
type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionObserveOnly     Decision = "observe_only"
	DecisionAlert           Decision = "alert"
	DecisionChallenge       Decision = "challenge"
	DecisionRequireApproval Decision = "require_approval"
	DecisionBlock           Decision = "block"
)

// Valid reports whether d is one of the six recognized Decision values.
func (d Decision) Valid() bool {
	switch d {
	case DecisionAllow, DecisionObserveOnly, DecisionAlert, DecisionChallenge, DecisionRequireApproval, DecisionBlock:
		return true
	default:
		return false
	}
}

// Input is what a Policy is evaluated against: the stable shape of the
// event, the Trust result computed from it, and the event's raw
// Attributes (for Condition.Attributes matching).
type Input struct {
	Stable     features.StableFeatures
	Trust      trust.Trust
	Attributes map[string]any
}

// Condition matches an Input. Every field is optional: its zero value
// ("" for strings/typed-strings) means "don't care" and never excludes a
// match. A Condition with every field zero matches everything.
//
// This means a Condition cannot itself distinguish "any target" from
// "specifically an empty target" — an acceptable MVP simplification,
// since real policies match on populated destinations/environments, not
// their absence.
type Condition struct {
	ActorType         event.ActorType
	OperationCategory event.OperationCategory
	TargetName        string
	Environment       string
	// MinRiskLevel, if set, requires Input.Trust.Risk to be at least this
	// severe (see trust.RiskLevel.AtLeast).
	MinRiskLevel trust.RiskLevel
	// Attributes, if non-empty, requires every key to be present in
	// Input.Attributes with a value whose string representation (via
	// fmt.Sprint) equals the configured value. A nil or empty map means
	// "don't care", consistent with every other field. This is what
	// closes the "tool.category: secrets" AI-agent policy example: no
	// AND/OR/NOT combinators, just flat key/value equality ANDed with
	// every other Condition field.
	Attributes map[string]string
}

// Matches reports whether every non-zero field of c matches in.
func (c Condition) Matches(in Input) bool {
	if c.ActorType != "" && c.ActorType != in.Stable.ActorType {
		return false
	}
	if c.OperationCategory != "" && c.OperationCategory != in.Stable.OperationCategory {
		return false
	}
	if c.TargetName != "" && c.TargetName != in.Stable.TargetName {
		return false
	}
	if c.Environment != "" && c.Environment != in.Stable.Environment {
		return false
	}
	if c.MinRiskLevel != "" && !in.Trust.Risk.AtLeast(c.MinRiskLevel) {
		return false
	}
	for key, want := range c.Attributes {
		got, ok := in.Attributes[key]
		if !ok || fmt.Sprint(got) != want {
			return false
		}
	}
	return true
}

// Rule is one entry in a Policy's ordered rule list: if When matches and
// Unless does not, Action applies.
type Rule struct {
	Name string
	When Condition
	// Unless, if non-nil, suppresses this Rule when it also matches —
	// e.g. a rule that blocks a category of access "unless" some
	// exception condition holds.
	Unless *Condition
	Action Decision
	// Reason is a static, human-readable explanation of why this rule
	// exists, surfaced on Explanation when the rule fires.
	Reason string
}

// Explanation is the audit trail for a Decision: which rule fired (or
// that the default applied) and why.
type Explanation struct {
	RuleName       string
	Reason         string
	MatchedDefault bool
}

// Result is the outcome of evaluating a Policy: always a valid Decision
// with a non-empty Explanation.
type Result struct {
	Decision    Decision
	Explanation Explanation
}

const failClosedReason = "no policy default configured; failing closed to BLOCK"

// Policy is an ordered list of Rules plus a mandatory default outcome
// for when none match. Policy is data: constructing one requires no
// code beyond a struct literal, and it is evaluated the same way
// regardless of where it came from (a Go literal today, a YAML loader
// later).
type Policy struct {
	Rules         []Rule
	DefaultAction Decision
	DefaultReason string
}

// Evaluate applies p's rules to in, first-match-wins, and returns the
// resulting Decision with its Explanation.
//
// If no Rule matches, DefaultAction/DefaultReason apply — but only if
// DefaultAction is itself a valid Decision and DefaultReason is
// non-empty. A zero-value or misconfigured Policy (e.g. one built
// without setting DefaultAction) fails closed to BLOCK rather than
// silently producing an empty or implicit Decision.
func (p Policy) Evaluate(in Input) Result {
	for _, rule := range p.Rules {
		if !rule.When.Matches(in) {
			continue
		}
		if rule.Unless != nil && rule.Unless.Matches(in) {
			continue
		}
		return Result{
			Decision: rule.Action,
			Explanation: Explanation{
				RuleName: rule.Name,
				Reason:   rule.Reason,
			},
		}
	}

	if !p.DefaultAction.Valid() || p.DefaultReason == "" {
		return Result{
			Decision: DecisionBlock,
			Explanation: Explanation{
				Reason:         failClosedReason,
				MatchedDefault: true,
			},
		}
	}

	return Result{
		Decision: p.DefaultAction,
		Explanation: Explanation{
			Reason:         p.DefaultReason,
			MatchedDefault: true,
		},
	}
}

// String renders a Result for logs/CLI output.
func (r Result) String() string {
	if r.Explanation.MatchedDefault {
		return fmt.Sprintf("%s (default: %s)", r.Decision, r.Explanation.Reason)
	}
	return fmt.Sprintf("%s (rule %q: %s)", r.Decision, r.Explanation.RuleName, r.Explanation.Reason)
}
