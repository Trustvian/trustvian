# Policy Guide

`internal/policy` is the last pipeline stage: it turns a `Trust` result
and an event's stable features into a `Decision`. A `Policy` is a plain
Go value — an ordered list of rules plus a mandatory default — not
code, so it can eventually be built from a YAML/file loader without
touching the evaluator.

> `Policy`/`Rule`/`Condition` currently live under `internal/`, so this
> guide's code is written the way `cmd/trustvian` itself builds its
> policy — usable by code inside this repository today. See
> [Go SDK Guide § the public/internal boundary today](sdk-guide.md#the-publicinternal-boundary-today).

## The types

```go
type Decision string
const (
	DecisionAllow           Decision = "allow"
	DecisionObserveOnly     Decision = "observe_only"
	DecisionAlert           Decision = "alert"
	DecisionChallenge       Decision = "challenge"
	DecisionRequireApproval Decision = "require_approval"
	DecisionBlock           Decision = "block"
)

type Condition struct {
	ActorType         event.ActorType         // "" = any
	OperationCategory event.OperationCategory // "" = any
	TargetName        string                  // "" = any
	Environment       string                  // "" = any
	MinRiskLevel      trust.RiskLevel         // "" = any; else Trust.Risk must be >= this
}

type Rule struct {
	Name   string
	When   Condition
	Unless *Condition // if it also matches, the rule is suppressed
	Action Decision
	Reason string
}

type Policy struct {
	Rules         []Rule
	DefaultAction Decision
	DefaultReason string
}
```

Every `Condition` field is optional — its zero value means "don't
care." A `Condition{}` matches every event. `MinRiskLevel` uses an
ordering (`RiskLow < RiskMedium < RiskHigh < RiskCritical`), so
`MinRiskLevel: trust.RiskMedium` matches `MEDIUM`, `HIGH`, and
`CRITICAL`.

## Evaluation: first match wins

```go
result := myPolicy.Evaluate(policy.Input{
	Stable: features.Stable, // from a Result, or built directly
	Trust:  trust,
})
// result.Decision, result.Explanation.{RuleName, Reason, MatchedDefault}
```

Rules are checked in order. The first `Rule` whose `When` matches
(and whose `Unless`, if set, does *not* match) wins — its `Action`
becomes `Decision`, its `Reason` becomes `Explanation.Reason`. If no
rule matches, `DefaultAction`/`DefaultReason` apply.

## Fail-closed, not fail-open

```go
var p policy.Policy // zero value: no rules, no default configured
result := p.Evaluate(in)
// result.Decision == policy.DecisionBlock
// result.Explanation.Reason == "no policy default configured; failing closed to BLOCK"
```

An unconfigured or misconfigured `Policy` — empty/invalid
`DefaultAction`, or `DefaultAction` set but `DefaultReason` left empty
— never falls through to `ALLOW` by accident. It fails closed to
`BLOCK`. There is no code path from "the config is wrong" to a silent
allow.

## Example: the CLI's starter policy

From [`cmd/trustvian/policy.go`](../cmd/trustvian/policy.go), real code
in this repository:

```go
policy.Policy{
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
```

This is what produces every `Decision` you see in
[Use Cases](use-cases.md) and the [CLI Guide](cli-guide.md) examples.

## Example: rule specificity (first match wins)

A more specific rule ahead of a catch-all:

```go
policy.Policy{
	Rules: []policy.Rule{
		{
			Name:   "block-secrets-access",
			When:   policy.Condition{TargetName: "secrets-manager"},
			Action: policy.DecisionBlock,
			Reason: "secrets access is always blocked, regardless of risk score",
		},
		{
			Name:   "block-high-risk",
			When:   policy.Condition{MinRiskLevel: trust.RiskHigh},
			Action: policy.DecisionBlock,
			Reason: "trust score indicates high or critical risk",
		},
	},
	DefaultAction: policy.DecisionAllow,
	DefaultReason: "risk within tolerance",
}
```

An event with `TargetName: "secrets-manager"` matches the first rule
and gets its specific reason, even if it would *also* have matched the
second, more general rule — order matters, and the first match wins.

## Example: `Unless` — an exception to a rule

Mirrors the project spec's own policy example ("block AI-agent secrets
access unless a human has approved"), expressed with the fields
available today (attribute-based conditions like `approval: human` need
a future policy loader — see the `Condition` doc comment in
[`internal/policy/policy.go`](../internal/policy/policy.go)):

```go
policy.Rule{
	Name:   "alert-external-calls",
	When:   policy.Condition{OperationCategory: event.OperationCategoryExternal},
	Unless: &policy.Condition{TargetName: "partner-api"}, // known, approved destination
	Action: policy.DecisionAlert,
	Reason: "external calls are unexpected for this actor",
}
```

An external call to `partner-api` is exempted (falls through to
whatever rule/default comes next); an external call to anything else
triggers the alert.

## Testing a policy

`Policy.Evaluate` is a pure function — table-driven tests are the
natural fit. See
[`internal/policy/policy_test.go`](../internal/policy/policy_test.go)
for the full pattern this codebase uses (rule ordering, `Unless`
override, all three fail-closed scenarios, and a check that every
`Result` across several policies has a non-empty `Explanation.Reason`).
