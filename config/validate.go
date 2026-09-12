package config

import (
	"errors"
	"fmt"
	"math"

	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/internal/policy"
)

// Sentinel errors, wrapped with fmt.Errorf and checked with errors.Is
// per .claude/rules/go.md's error-handling convention. Each identifies
// a distinct validation failure category; Validate wraps the specific
// one that applies with the exact field path that triggered it.
var (
	ErrUnsupportedVersion      = errors.New("config: unsupported policy config version")
	ErrMissingDefault          = errors.New("config: default_decision and default_reason must both be set")
	ErrInvalidDecision         = errors.New("config: invalid decision")
	ErrInvalidActorType        = errors.New("config: invalid actor_type")
	ErrInvalidCategory         = errors.New("config: invalid operation_category")
	ErrInvalidRiskLevel        = errors.New("config: invalid min_risk_level")
	ErrEmptyRuleName           = errors.New("config: rule name must not be empty")
	ErrDuplicateRuleName       = errors.New("config: duplicate rule name")
	ErrMissingReason           = errors.New("config: rule reason must not be empty")
	ErrTooManyRules            = errors.New("config: too many rules")
	ErrNameTooLong             = errors.New("config: name exceeds maximum length")
	ErrInvalidSeverity         = errors.New("config: invalid severity")
	ErrInvalidTargetCategory   = errors.New("config: invalid target_category")
	ErrInvalidThreshold        = errors.New("config: threshold must be a finite number in [0, 1]")
	ErrUnsupportedAlertVersion = errors.New("config: unsupported alert config version")
	ErrInvalidApprovalStatus   = errors.New("config: invalid approval_status")

	ErrUnsupportedAnomalyVersion = errors.New("config: unsupported anomaly config version")
	// ErrInvalidZThreshold is distinct from ErrInvalidThreshold: a
	// z-score threshold is a standard-deviation multiple (e.g. 3.0),
	// never bounded to [0, 1] the way a weight/probability-like
	// threshold is — see anomaly.Config.LatencyZThreshold/
	// FrequencyZThreshold's own "Must be > 0" doc comments.
	ErrInvalidZThreshold = errors.New("config: z-threshold must be a finite number greater than 0")
)

// Bounds on config-authored input. Configuration is operator-authored
// but still untrusted operational input — the same "resource
// exhaustion" threat category docs/SECURITY.md already documents for
// runtime Attributes maps applies here, scaled to what a legitimate
// policy file could ever plausibly need. These are deliberately small
// relative to runtime bounds elsewhere in this codebase (e.g. the
// 100,000-key Attributes map internal/security's tests probe): a
// config file is human-authored operational input, not per-event
// telemetry volume, so a much stricter cap is appropriate and does
// not constrain any real use case.
const (
	// maxRules bounds PolicyConfig.Rules. 1000 rules is far beyond
	// what a hand-authored or generated policy file plausibly
	// contains; evaluation is already O(rules) per Analyze call
	// (first-match-wins, see policy.Policy.Evaluate), so this also
	// bounds the worst-case per-event evaluation cost a config file
	// can introduce.
	maxRules = 1000

	// maxNameLength bounds PolicyRule.Name and PolicyRule.Reason —
	// both are surfaced verbatim in Explanation and, potentially,
	// logs; an unbounded name is a resource-exhaustion vector with no
	// legitimate use (a rule name is an identifier, not free text).
	maxNameLength = 256
)

// validActorTypes mirrors event.ActorType's own valid() method
// (event/event.go) — that method exists but is unexported, so this
// package (which can import the public event package but not reach
// into its unexported methods) cannot call it directly; this list
// must be kept in sync with valid()'s case values if they ever
// change. See event.ActorType's own doc comment for the authoritative
// source of these six values.
var validActorTypes = map[string]bool{
	"service":         true,
	"user":            true,
	"service_account": true,
	"ai_agent":        true,
	"device":          true,
	"unknown":         true,
}

// validOperationCategories mirrors event.OperationCategory's own
// unexported valid() method — same reason as validActorTypes above.
var validOperationCategories = map[string]bool{
	"http":     true,
	"db":       true,
	"rpc":      true,
	"tool":     true,
	"external": true,
}

// validApprovalStatuses mirrors event.ApprovalStatus's four non-empty
// constants (ApprovalUnspecified, the zero value, is deliberately
// excluded: like every other Condition field, "" means "don't care",
// not "specifically match Unspecified" — see
// policy.Condition.ApprovalStatus's own doc comment). Unlike
// ActorType/OperationCategory/TargetCategory, event.ApprovalStatus has
// no unexported valid() method for this package to mirror — the five
// constants declared in event/event.go are themselves the
// authoritative set; this map is this package's own validity check
// against them.
var validApprovalStatuses = map[string]bool{
	"not_required": true,
	"required":     true,
	"approved":     true,
	"denied":       true,
}

// validRiskLevels mirrors the four trust.RiskLevel constants —
// trust.RiskLevel.AtLeast does not reject an unrecognized value (its
// own doc comment: "an unrecognized value ranks below RiskLow"), so it
// cannot be used to detect a typo'd risk level; this package cannot
// import internal/trust's unexported riskRank map either, so this
// list is this package's own authoritative validity check.
var validRiskLevels = map[string]bool{
	"low":      true,
	"medium":   true,
	"high":     true,
	"critical": true,
}

// Validate reports whether cfg is a well-formed PolicyConfig: a
// recognized schema version, a valid, complete default, and a Rules
// list where every rule has a unique, non-empty, bounded-length name,
// a valid Decision, a non-empty Reason, and a well-formed
// PolicyCondition. Validate returns the first problem it finds — the
// same "first error wins" convention event.Event.Validate already
// uses in this codebase, rather than an aggregate of every problem.
//
// Validate is called internally by CompilePolicy, but is also exposed
// standalone so a caller (a future "policy test"-style CLI command,
// for instance) can validate a config without compiling it.
func (cfg PolicyConfig) Validate() error {
	if cfg.Version != SchemaVersionV1 {
		return fmt.Errorf("%w: %q (supported: %q)", ErrUnsupportedVersion, cfg.Version, SchemaVersionV1)
	}

	if cfg.DefaultDecision == "" || cfg.DefaultReason == "" {
		return ErrMissingDefault
	}
	if !policy.Decision(cfg.DefaultDecision).Valid() {
		return fmt.Errorf("default_decision: %w: %q", ErrInvalidDecision, cfg.DefaultDecision)
	}

	if len(cfg.Rules) > maxRules {
		return fmt.Errorf("rules: %w: %d (max %d)", ErrTooManyRules, len(cfg.Rules), maxRules)
	}

	seenNames := make(map[string]bool, len(cfg.Rules))
	for i, r := range cfg.Rules {
		if err := r.validate(i); err != nil {
			return err
		}
		if seenNames[r.Name] {
			return fmt.Errorf("rules[%d].name: %w: %q", i, ErrDuplicateRuleName, r.Name)
		}
		seenNames[r.Name] = true
	}

	return nil
}

func (r PolicyRule) validate(i int) error {
	if r.Name == "" {
		return fmt.Errorf("rules[%d].name: %w", i, ErrEmptyRuleName)
	}
	if len(r.Name) > maxNameLength {
		return fmt.Errorf("rules[%d].name: %w: %d chars (max %d)", i, ErrNameTooLong, len(r.Name), maxNameLength)
	}
	if r.Reason == "" {
		return fmt.Errorf("rules[%d].reason: %w", i, ErrMissingReason)
	}
	if len(r.Reason) > maxNameLength {
		return fmt.Errorf("rules[%d].reason: %w: %d chars (max %d)", i, ErrNameTooLong, len(r.Reason), maxNameLength)
	}
	if !policy.Decision(r.Decision).Valid() {
		return fmt.Errorf("rules[%d].decision: %w: %q", i, ErrInvalidDecision, r.Decision)
	}
	if err := r.When.validate(fmt.Sprintf("rules[%d].when", i)); err != nil {
		return err
	}
	if r.Unless != nil {
		if err := r.Unless.validate(fmt.Sprintf("rules[%d].unless", i)); err != nil {
			return err
		}
	}
	return nil
}

func (c PolicyCondition) validate(path string) error {
	if c.ActorType != "" && !validActorTypes[c.ActorType] {
		return fmt.Errorf("%s.actor_type: %w: %q", path, ErrInvalidActorType, c.ActorType)
	}
	if c.OperationCategory != "" && !validOperationCategories[c.OperationCategory] {
		return fmt.Errorf("%s.operation_category: %w: %q", path, ErrInvalidCategory, c.OperationCategory)
	}
	if c.MinRiskLevel != "" && !validRiskLevels[c.MinRiskLevel] {
		return fmt.Errorf("%s.min_risk_level: %w: %q", path, ErrInvalidRiskLevel, c.MinRiskLevel)
	}
	if c.ApprovalStatus != "" && !validApprovalStatuses[c.ApprovalStatus] {
		return fmt.Errorf("%s.approval_status: %w: %q", path, ErrInvalidApprovalStatus, c.ApprovalStatus)
	}
	return nil
}

// validTargetCategories mirrors event.TargetCategory's own unexported
// valid() method's three non-empty values — same reason
// validActorTypes/validOperationCategories mirror their event.* enums:
// this package can import the public event package but not reach its
// unexported methods.
var validTargetCategories = map[string]bool{
	"internal": true,
	"external": true,
	"database": true,
}

// Validate reports whether cfg is a well-formed AlertConfig: a
// recognized schema version and a Rules list where every rule has a
// unique, non-empty, bounded-length name, a valid Severity, and a
// well-formed AlertConditionConfig. Validate returns the first problem
// it finds, mirroring PolicyConfig.Validate's own "first error wins"
// convention.
//
// Unlike PolicyConfig, there is no default/reason to require: an empty
// Rules list is a valid (if inert) AlertConfig, matching
// alert.Evaluate's own "no match means no alert, not a fail-closed
// error" asymmetry with policy.Policy.Evaluate.
//
// Validate is called internally by CompileAlerts, but is also exposed
// standalone for the same reason PolicyConfig.Validate is: a caller
// can validate a config without compiling it.
func (cfg AlertConfig) Validate() error {
	if cfg.Version != AlertSchemaVersionV1 {
		return fmt.Errorf("%w: %q (supported: %q)", ErrUnsupportedAlertVersion, cfg.Version, AlertSchemaVersionV1)
	}

	if len(cfg.Rules) > maxRules {
		return fmt.Errorf("rules: %w: %d (max %d)", ErrTooManyRules, len(cfg.Rules), maxRules)
	}

	seenNames := make(map[string]bool, len(cfg.Rules))
	for i, r := range cfg.Rules {
		if err := r.validate(i); err != nil {
			return err
		}
		if seenNames[r.Name] {
			return fmt.Errorf("rules[%d].name: %w: %q", i, ErrDuplicateRuleName, r.Name)
		}
		seenNames[r.Name] = true
	}

	return nil
}

func (r AlertRuleConfig) validate(i int) error {
	if r.Name == "" {
		return fmt.Errorf("rules[%d].name: %w", i, ErrEmptyRuleName)
	}
	if len(r.Name) > maxNameLength {
		return fmt.Errorf("rules[%d].name: %w: %d chars (max %d)", i, ErrNameTooLong, len(r.Name), maxNameLength)
	}
	if !alert.Severity(r.Severity).Valid() {
		return fmt.Errorf("rules[%d].severity: %w: %q", i, ErrInvalidSeverity, r.Severity)
	}
	return r.When.validate(fmt.Sprintf("rules[%d].when", i))
}

func (c AlertConditionConfig) validate(path string) error {
	if c.Decision != "" && !policy.Decision(c.Decision).Valid() {
		return fmt.Errorf("%s.decision: %w: %q", path, ErrInvalidDecision, c.Decision)
	}
	if c.MinRiskLevel != "" && !validRiskLevels[c.MinRiskLevel] {
		return fmt.Errorf("%s.min_risk_level: %w: %q", path, ErrInvalidRiskLevel, c.MinRiskLevel)
	}
	if c.ActorType != "" && !validActorTypes[c.ActorType] {
		return fmt.Errorf("%s.actor_type: %w: %q", path, ErrInvalidActorType, c.ActorType)
	}
	if c.TargetCategory != "" && !validTargetCategories[c.TargetCategory] {
		return fmt.Errorf("%s.target_category: %w: %q", path, ErrInvalidTargetCategory, c.TargetCategory)
	}
	if !validThreshold(c.MinAnomalyScore) {
		return fmt.Errorf("%s.min_anomaly_score: %w: %v", path, ErrInvalidThreshold, c.MinAnomalyScore)
	}
	if c.MaxTrustScore != nil && !validThreshold(*c.MaxTrustScore) {
		return fmt.Errorf("%s.max_trust_score: %w: %v", path, ErrInvalidThreshold, *c.MaxTrustScore)
	}
	return nil
}

// validThreshold reports whether v is finite (never NaN or ±Inf) and
// within [0, 1] — the documented range both Anomaly.Score and
// Trust.Score are always bounded to (see docs/DOMAIN.md), so a
// threshold outside that range could never match anything meaningful
// and is rejected as a config mistake rather than silently accepted as
// a threshold that can never fire (MinAnomalyScore) or always fires
// (MaxTrustScore).
func validThreshold(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}

// validZThreshold reports whether v is finite and strictly positive —
// the documented range every anomaly.Config z-score threshold field
// requires ("Must be > 0"). Weights use validThreshold's [0, 1] range;
// a z-threshold is a standard-deviation multiple, an unrelated scale.
func validZThreshold(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0
}

// Validate reports whether cfg is a well-formed AnomalyConfig: a
// recognized schema version and every set field within its documented
// range. Validate returns the first problem it finds, mirroring
// PolicyConfig.Validate/AlertConfig.Validate's own "first error wins"
// convention.
//
// Validate is called internally by CompileAnomaly, but is also exposed
// standalone for the same reason PolicyConfig.Validate/
// AlertConfig.Validate are: a caller can validate a config without
// compiling it.
func (cfg AnomalyConfig) Validate() error {
	if cfg.Version != AnomalySchemaVersionV1 {
		return fmt.Errorf("%w: %q (supported: %q)", ErrUnsupportedAnomalyVersion, cfg.Version, AnomalySchemaVersionV1)
	}

	// MinObservations has no invalid uint64 value: 0 is a genuine,
	// internally-handled configuration ("everything is immediately
	// mature" — see anomaly.Score's own familiarity computation), not
	// an error, and every other uint64 value is equally well-defined.
	if cfg.LatencyZThreshold != nil && !validZThreshold(*cfg.LatencyZThreshold) {
		return fmt.Errorf("latency_z_threshold: %w: %v", ErrInvalidZThreshold, *cfg.LatencyZThreshold)
	}
	if cfg.FrequencyZThreshold != nil && !validZThreshold(*cfg.FrequencyZThreshold) {
		return fmt.Errorf("frequency_z_threshold: %w: %v", ErrInvalidZThreshold, *cfg.FrequencyZThreshold)
	}
	if cfg.NoveltyWeight != nil && !validThreshold(*cfg.NoveltyWeight) {
		return fmt.Errorf("novelty_weight: %w: %v", ErrInvalidThreshold, *cfg.NoveltyWeight)
	}
	if cfg.LatencyWeight != nil && !validThreshold(*cfg.LatencyWeight) {
		return fmt.Errorf("latency_weight: %w: %v", ErrInvalidThreshold, *cfg.LatencyWeight)
	}
	if cfg.ErrorWeight != nil && !validThreshold(*cfg.ErrorWeight) {
		return fmt.Errorf("error_weight: %w: %v", ErrInvalidThreshold, *cfg.ErrorWeight)
	}
	if !validThreshold(cfg.FrequencyWeight) {
		return fmt.Errorf("frequency_weight: %w: %v", ErrInvalidThreshold, cfg.FrequencyWeight)
	}
	if !validThreshold(cfg.TimePatternWeight) {
		return fmt.Errorf("time_pattern_weight: %w: %v", ErrInvalidThreshold, cfg.TimePatternWeight)
	}
	if !validThreshold(cfg.TransitionWeight) {
		return fmt.Errorf("transition_weight: %w: %v", ErrInvalidThreshold, cfg.TransitionWeight)
	}
	// MinTransitionObservations, like MinObservations above, has no
	// invalid uint64 value — 0 means "any nonzero total is enough" in
	// transitionRaritySignal/markovSurprisalSignal, a valid, if
	// extreme, configuration.
	if !validThreshold(cfg.TransitionRarityWeight) {
		return fmt.Errorf("transition_rarity_weight: %w: %v", ErrInvalidThreshold, cfg.TransitionRarityWeight)
	}
	if !validThreshold(cfg.NGramWeight) {
		return fmt.Errorf("ngram_weight: %w: %v", ErrInvalidThreshold, cfg.NGramWeight)
	}
	// MinNGramObservations: same reasoning as MinTransitionObservations
	// above — 0 is a valid, extreme configuration, not an error.
	if !validThreshold(cfg.NGramRarityWeight) {
		return fmt.Errorf("ngram_rarity_weight: %w: %v", ErrInvalidThreshold, cfg.NGramRarityWeight)
	}
	if !validThreshold(cfg.MarkovWeight) {
		return fmt.Errorf("markov_weight: %w: %v", ErrInvalidThreshold, cfg.MarkovWeight)
	}
	if !validThreshold(cfg.DelegationWeight) {
		return fmt.Errorf("delegation_weight: %w: %v", ErrInvalidThreshold, cfg.DelegationWeight)
	}
	for target, floor := range cfg.SensitiveTargetFloor {
		if !validThreshold(floor) {
			return fmt.Errorf("sensitive_target_floor[%q]: %w: %v", target, ErrInvalidThreshold, floor)
		}
	}

	return nil
}
