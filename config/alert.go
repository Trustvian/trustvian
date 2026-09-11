// Alert configuration is deliberately a separate document and a
// separate compiled artifact from PolicyConfig — see
// docs/adr/0009-alert-config-is-a-separate-document.md for the full
// reasoning. In short: Policy answers "what decision should Trustvian
// make," Alert configuration answers "which Results/Decisions should
// produce an Alert" (see docs/DOMAIN.md § Policy and Decision and
// trustvian-project-spec.md § 18.1); they are independently compiled
// today (PolicyConfig -> CompilePolicy -> policy.Policy,
// AlertConfig -> CompileAlerts -> []alert.Rule) and this package keeps
// that independence at the configuration-document level too, rather
// than introducing a combined schema before a real consumer needs one.

package config

// AlertSchemaVersionV1 is the only AlertConfig schema version this
// package currently accepts. Independently versioned from
// SchemaVersionV1 (PolicyConfig's own version) on purpose — see
// SchemaVersionV1's doc comment and ADR 0008 § Schema versioning for
// why a config schema version and a Trustvian release version are
// deliberately decoupled; the same reasoning applies to keeping
// PolicyConfig's and AlertConfig's schema version numbers independent
// of each other, even though both currently read "v1".
const AlertSchemaVersionV1 = "v1"

// AlertConfig is the public, declarative description of an Alert
// Evaluation rule set — a []alert.Rule, compiled and validated by
// CompileAlerts. It mirrors PolicyConfig's own shape and conventions
// deliberately (Version + an ordered Rules slice, first-match-wins),
// but is a structurally independent type: nothing in this package
// merges PolicyConfig and AlertConfig into one generic rule language.
type AlertConfig struct {
	// Version must be AlertSchemaVersionV1 today. Required, not
	// defaulted — same reasoning as PolicyConfig.Version.
	Version string `yaml:"version"`

	// Rules is evaluated first-match-wins, in slice order — the same
	// semantics alert.Evaluate already has, and the same "no reorder,
	// no map, deterministic by construction" guarantee CompilePolicy
	// already gives PolicyConfig.Rules. Unlike PolicyConfig, there is
	// no default/fallback rule: alert.Evaluate's own documented
	// asymmetry with policy.Policy.Evaluate is that no match simply
	// means "no alert," a safe, inert outcome — so AlertConfig has no
	// DefaultSeverity field to omit-or-require.
	Rules []AlertRuleConfig `yaml:"rules"`
}

// AlertRuleConfig is one entry in AlertConfig.Rules: if When matches a
// Result, an Alert at Severity is produced. Mirrors alert.Rule's
// semantics exactly — this type does not add anything alert.Rule
// cannot already express, and does not add the Unless-suppression or
// Reason fields policy.Rule/PolicyRule have, because alert.Rule itself
// has neither.
//
// Named AlertRuleConfig, not AlertRule (unlike PolicyRule, which has
// no such suffix) — a deliberate naming asymmetry, not an
// inconsistency: internal/policy is unexported, so a PolicyRule value
// is the only "Rule"-named symbol any external caller's own source
// ever has in scope. alert, by contrast, is itself a public package —
// a real caller compiling Alert config almost always also imports
// alert directly (to call alert.Evaluate on the compiled result), so
// config.AlertRule sitting next to alert.Rule in the same file would
// read as two different things with confusingly similar names. The
// same reasoning applies to AlertConditionConfig vs. PolicyCondition
// below.
type AlertRuleConfig struct {
	// Name identifies this rule for operator-facing purposes (it does
	// not appear in the compiled alert.Rule's own matching behavior,
	// but is required and validated the same way PolicyRule.Name is —
	// for the same explainability and config-hygiene reasons: a
	// missing or duplicate name is a config mistake worth catching at
	// compile time, not silently accepted).
	Name string `yaml:"name"`

	When AlertConditionConfig `yaml:"when"`

	// Severity must be one of the five recognized alert.Severity
	// values.
	Severity string `yaml:"severity"`
}

// AlertConditionConfig matches the same input alert.Condition already
// matches — a flat set of independent, optional fields, ANDed
// together, whose zero value matches everything, exactly like
// alert.Condition's own zero value. It intentionally exposes only the
// matching dimensions alert.Condition already supports today; adding a
// new one is a separate, future decision about alert.Condition itself,
// not something this configuration layer should get ahead of (see
// .claude/rules/architecture.md's "abstractions that should not exist
// here" discipline, applied identically to how config/policy.go's own
// PolicyCondition already treats policy.Condition).
type AlertConditionConfig struct {
	// Decision, if set, must be one of the six recognized Decision
	// values and requires an exact match against Result.Decision.
	Decision string `yaml:"decision,omitempty"`

	// MinRiskLevel, if set, must be one of "low", "medium", "high",
	// "critical" — identical semantics and validity to
	// PolicyCondition.MinRiskLevel.
	MinRiskLevel string `yaml:"min_risk_level,omitempty"`

	// ActorType, if set, must be one of the recognized
	// event.ActorType values.
	ActorType string `yaml:"actor_type,omitempty"`

	// TargetCategory, if set, must be one of "internal", "external",
	// "database" (event.TargetCategory's three non-empty values).
	TargetCategory string `yaml:"target_category,omitempty"`

	// MinAnomalyScore, if set, must be a finite value in [0, 1] — the
	// same documented range Anomaly.Score itself is always bounded to
	// (see docs/DOMAIN.md). Its zero value (0) is a genuine "don't
	// care", matching alert.Condition.MinAnomalyScore's own zero-value
	// semantics, so this field needs no pointer/omitempty distinction
	// the way MaxTrustScore does below.
	MinAnomalyScore float64 `yaml:"min_anomaly_score,omitempty"`

	// MaxTrustScore, if non-nil, must be a finite value in [0, 1] —
	// the same documented range Trust.Score is always bounded to. A
	// pointer, not a plain float64, for the identical reason
	// alert.Condition.MaxTrustScore is a pointer: a plain zero value
	// here would mean "match only Trust.Score == 0" (the most
	// restrictive possible threshold), not "don't care" — the
	// opposite of every other field's zero-value meaning.
	MaxTrustScore *float64 `yaml:"max_trust_score,omitempty"`
}
