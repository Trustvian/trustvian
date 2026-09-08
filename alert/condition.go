package alert

import (
	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

// Condition matches a trustvian.Result. It follows the exact discipline
// internal/policy.Condition already established for matching Decision
// evaluation input: every field is optional, its zero value means
// "don't care" and never excludes a match, and there are no
// AND/OR/NOT combinators — this is deliberately the same flat,
// first-match-wins shape, not a general expression language (see
// docs/tasks/018-alert-notification-foundation.md § Non-Goals).
//
// This type does not import or reuse policy.Condition itself: Alert
// Evaluation is a structurally independent, read-only consumer of
// Result, not a second stage of decision-making layered onto Policy.
type Condition struct {
	// Decision, if set, requires an exact match against Result.Decision.
	Decision policy.Decision

	// MinRiskLevel, if set, requires Result.Trust.Risk to be at least
	// this severe (trust.RiskLevel.AtLeast) — the same semantics
	// policy.Condition.MinRiskLevel already uses.
	MinRiskLevel trust.RiskLevel

	// ActorType, if set, requires an exact match against
	// Result.Event.Actor.Type.
	ActorType event.ActorType

	// TargetCategory, if set, requires an exact match against
	// Result.Event.Target.Category.
	TargetCategory event.TargetCategory

	// MinAnomalyScore requires Result.Anomaly.Score to be at least this
	// value. Its zero value (0) is a genuine "don't care": every
	// Anomaly.Score is already >= 0, so a zero threshold excludes
	// nothing, matching this Condition's "zero value never excludes a
	// match" invariant without needing a pointer.
	MinAnomalyScore float64

	// MaxTrustScore, if non-nil, requires Result.Trust.Score to be at
	// most this value. Unlike MinAnomalyScore, a plain float64 zero
	// value here would be the *most* restrictive possible threshold
	// (matching only a Trust.Score of exactly 0), not "don't care" — the
	// opposite of every other field's zero-value meaning. A pointer is
	// used specifically to keep the "zero value means don't care"
	// invariant intact: nil, not 0.0, is "don't care" here.
	MaxTrustScore *float64
}

// Matches reports whether every set field of c matches result.
func (c Condition) Matches(result trustvian.Result) bool {
	if c.Decision != "" && c.Decision != result.Decision {
		return false
	}
	if c.MinRiskLevel != "" && !result.Trust.Risk.AtLeast(c.MinRiskLevel) {
		return false
	}
	if c.ActorType != "" && c.ActorType != result.Event.Actor.Type {
		return false
	}
	if c.TargetCategory != "" && c.TargetCategory != result.Event.Target.Category {
		return false
	}
	if result.Anomaly.Score < c.MinAnomalyScore {
		return false
	}
	if c.MaxTrustScore != nil && result.Trust.Score > *c.MaxTrustScore {
		return false
	}
	return true
}

// Rule pairs a Condition with the Severity an Alert should carry when
// it matches.
type Rule struct {
	Name     string
	When     Condition
	Severity Severity
}

// Evaluate runs result against rules in order and returns the Alert
// produced by the first matching Rule. It returns (Alert{}, false) when
// no Rule matches — a Decision never automatically implies an Alert.
//
// Unlike policy.Policy.Evaluate, there is no mandatory default and no
// fail-closed requirement: an empty or entirely non-matching rule set
// simply means "no alert," which is a safe, inert outcome (the absence
// of a notification is not a security regression the way an
// unconfigured Policy silently falling open would be). This is a
// deliberate, documented asymmetry with policy.Policy.Evaluate, not an
// oversight.
func Evaluate(result trustvian.Result, rules []Rule) (Alert, bool) {
	for _, r := range rules {
		if r.When.Matches(result) {
			return New(result, r.Severity), true
		}
	}
	return Alert{}, false
}
