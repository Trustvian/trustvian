// Package config is Trustvian's public, declarative configuration
// boundary — the "Policy & Configuration" milestone
// (docs/ROADMAP.md § v0.5). It exists so a caller can express
// meaningful Policy behavior without writing Go code inside this
// module or importing internal/policy, closing the gap ADR 0002
// documented: today only code living inside this module can
// construct a policy.Policy at all.
//
// This package does not expose internal/policy's types. It defines
// its own, primitive-typed public structs (PolicyConfig, PolicyRule,
// ConditionConfig) and one compiler, CompilePolicy, that validates and
// translates them into a policy.Policy — the exact type
// trustvian.WithPolicy accepts. A caller in a genuinely separate
// module (the CLI, the OTel Collector processor, a standalone
// deployment) can call CompilePolicy and pass its result straight into
// trustvian.WithPolicy without ever importing internal/policy: Go
// allows receiving and passing through a value of an unexported-to-you
// type via type inference, as long as the type name itself is never
// spelled out in the caller's own source. See ADR 0008 for the full
// reasoning and the empirical verification behind this design.
//
// Configuration is declarative input describing desired behavior. It
// is never runtime state: nothing in this package holds a Baseline,
// a Fingerprint, learned statistics, or alert delivery state — that
// remains the Store's and the Engine's responsibility, unaffected by
// this package's existence.
package config

// SchemaVersionV1 is the only PolicyConfig schema version this
// package currently accepts. It is a config schema version, not a
// Trustvian release version — the two are deliberately independent:
// Trustvian v0.5.0 may consume config schema v1 today, and a future
// Trustvian v0.9.0 may still consume the same schema v1, or a newer
// v2, without those two version numbers being tied together. See
// ADR 0008 § Schema versioning.
const SchemaVersionV1 = "v1"

// PolicyConfig is the public, declarative description of a
// policy.Policy. It is validated and translated by CompilePolicy;
// nothing in this type performs evaluation itself.
type PolicyConfig struct {
	// Version must be SchemaVersionV1 today. Required, not defaulted:
	// an empty or unrecognized Version fails validation rather than
	// silently assuming the current schema, so a config file written
	// against a future, incompatible schema version fails loudly
	// instead of being silently misinterpreted.
	Version string `yaml:"version"`

	// DefaultDecision and DefaultReason apply when no Rule matches.
	// Both are required and validated at compile time, deliberately
	// stricter than policy.Policy.Evaluate's own runtime fail-closed
	// behavior (an unconfigured policy.Policy fails closed to BLOCK
	// with a generic reason): a config-time omission gets a specific,
	// actionable error referencing the config itself, rather than
	// silently compiling into a Policy that only reveals the mistake
	// once Evaluate runs. DefaultDecision must be one of the six
	// recognized Decision values (see docs/DOMAIN.md § Policy and
	// Decision).
	DefaultDecision string `yaml:"default_decision"`
	DefaultReason   string `yaml:"default_reason"`

	// Rules is evaluated first-match-wins, in slice order — the same
	// semantics policy.Policy.Evaluate already has. CompilePolicy
	// preserves this order exactly; it never reorders, sorts, or
	// iterates Rules through anything that could reorder them (no map
	// involved), so compilation is deterministic by construction. An
	// empty Rules slice is valid: DefaultDecision/DefaultReason alone
	// describe a legitimate policy (this is exactly the shape
	// trustvian.NewEngine()'s own built-in default Policy already
	// has). YAML sequence order is preserved by the decoder — proven
	// by TestLoadPreservesRuleOrder, not just assumed of the library.
	Rules []PolicyRule `yaml:"rules"`
}

// PolicyRule is one entry in PolicyConfig.Rules: if When matches and
// Unless does not, Decision applies. Mirrors policy.Rule's semantics
// exactly — this type does not add anything policy.Rule cannot
// already express.
type PolicyRule struct {
	// Name identifies this rule in Explanation.RuleName when it
	// fires. Required and must be unique within a PolicyConfig — see
	// Validate for why both are enforced even though
	// policy.Policy.Evaluate itself would still function with an
	// empty or duplicate name (explainability, not evaluation
	// correctness, is what a missing/duplicate name breaks).
	Name string `yaml:"name"`

	When PolicyCondition `yaml:"when"`

	// Unless, if non-nil, suppresses this rule when it also matches —
	// identical to policy.Rule.Unless.
	Unless *PolicyCondition `yaml:"unless,omitempty"`

	// Decision must be one of the six recognized Decision values.
	Decision string `yaml:"decision"`

	// Reason is surfaced in Explanation.Reason when this rule fires.
	// Required, for the same explainability reason Name is required.
	Reason string `yaml:"reason"`
}

// PolicyCondition matches the same input a policy.Condition already
// matches — a flat set of independent, optional fields, ANDed
// together. Its zero value matches everything, exactly like
// policy.Condition's. It intentionally exposes only the matching
// dimensions policy.Condition already supports today
// (ActorType, OperationCategory, TargetName, Environment,
// MinRiskLevel, Attributes, ApprovalStatus) — it does not invent new
// matchable dimensions (e.g. a numeric anomaly-score or trust-score
// threshold) that the underlying engine cannot yet evaluate; extending
// policy.Condition itself is a separate, future decision, not
// something this configuration layer should get ahead of.
type PolicyCondition struct {
	// ActorType, if set, must be one of the recognized
	// event.ActorType values ("service", "user", "service_account",
	// "ai_agent", "device", "unknown").
	ActorType string `yaml:"actor_type,omitempty"`

	// OperationCategory, if set, must be one of the recognized
	// event.OperationCategory values ("http", "db", "rpc", "tool",
	// "external").
	OperationCategory string `yaml:"operation_category,omitempty"`

	// TargetName, if set, matches exactly — no wildcard, no prefix
	// matching, identical to policy.Condition.TargetName.
	TargetName string `yaml:"target_name,omitempty"`

	// Environment, if set, matches exactly.
	Environment string `yaml:"environment,omitempty"`

	// MinRiskLevel, if set, must be one of "low", "medium", "high",
	// "critical", and requires the evaluated Trust.Risk to be at
	// least this severe (trust.RiskLevel.AtLeast's exact semantics).
	MinRiskLevel string `yaml:"min_risk_level,omitempty"`

	// Attributes, if non-empty, requires every key to be present with
	// a matching value — identical to policy.Condition.Attributes.
	Attributes map[string]string `yaml:"attributes,omitempty"`

	// ApprovalStatus, if set, must be one of the recognized
	// event.ApprovalStatus values ("not_required", "required",
	// "approved", "denied") and requires the event's approval evidence
	// to equal it exactly — identical to
	// policy.Condition.ApprovalStatus. Pair this with Unless to express
	// "this operation requires approval": When matches the operation,
	// Unless matches ApprovalStatus: "approved", so the rule fires for
	// every other approval state — see
	// docs/tasks/030-approval-aware-policy-semantics.md.
	ApprovalStatus string `yaml:"approval_status,omitempty"`
}
