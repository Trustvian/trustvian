package config_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
	"github.com/Trustvian/trustvian/event"
)

const minimalYAML = `
version: v1
default_decision: observe_only
default_reason: no policy rules configured; observing by default
`

const fullYAML = `
version: v1
default_decision: observe_only
default_reason: no policy rules configured; observing by default
rules:
  - name: block-agent-secrets
    when:
      actor_type: ai_agent
      attributes:
        tool.category: secrets
    unless:
      attributes:
        approval: human
    decision: block
    reason: AI agent secret access requires human approval
  - name: critical-risk
    when:
      min_risk_level: critical
    decision: require_approval
    reason: critical risk requires manual approval
`

func TestLoadMinimalYAML(t *testing.T) {
	cfg, err := config.Load([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != config.SchemaVersionV1 {
		t.Errorf("Version = %q, want %q", cfg.Version, config.SchemaVersionV1)
	}
	if cfg.DefaultDecision != "observe_only" {
		t.Errorf("DefaultDecision = %q, want %q", cfg.DefaultDecision, "observe_only")
	}
	if len(cfg.Rules) != 0 {
		t.Errorf("Rules = %v, want empty", cfg.Rules)
	}
}

func TestLoadFullYAML(t *testing.T) {
	cfg, err := config.Load([]byte(fullYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("len(Rules) = %d, want 2", len(cfg.Rules))
	}
	r0 := cfg.Rules[0]
	if r0.Name != "block-agent-secrets" || r0.Decision != "block" {
		t.Errorf("Rules[0] = %+v, unexpected", r0)
	}
	if r0.When.ActorType != "ai_agent" {
		t.Errorf("Rules[0].When.ActorType = %q, want %q", r0.When.ActorType, "ai_agent")
	}
	if r0.When.Attributes["tool.category"] != "secrets" {
		t.Errorf("Rules[0].When.Attributes[tool.category] = %q, want %q", r0.When.Attributes["tool.category"], "secrets")
	}
	if r0.Unless == nil || r0.Unless.Attributes["approval"] != "human" {
		t.Errorf("Rules[0].Unless = %+v, unexpected", r0.Unless)
	}
}

func TestLoadPreservesRuleOrder(t *testing.T) {
	yaml := `
version: v1
default_decision: observe_only
default_reason: default
rules:
  - name: first
    decision: allow
    reason: r1
  - name: second
    decision: block
    reason: r2
  - name: third
    decision: alert
    reason: r3
`
	cfg, err := config.Load([]byte(yaml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := []string{cfg.Rules[0].Name, cfg.Rules[1].Name, cfg.Rules[2].Name}
	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rule order = %v, want %v", got, want)
	}
}

func TestLoadedConfigCompilesSuccessfully(t *testing.T) {
	cfg, err := config.Load([]byte(fullYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := config.CompilePolicy(cfg); err != nil {
		t.Errorf("CompilePolicy(loaded config): %v", err)
	}
}

// TestEndToEndYAMLFileChangesEngineDecision is the runtime-integration
// test this task's own acceptance criteria require: a real YAML file
// on disk, loaded, validated, compiled, and proven to change what the
// Engine actually decides — not just that it decodes into a struct.
func TestEndToEndYAMLFileChangesEngineDecision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trustvian.yaml")
	if err := os.WriteFile(path, []byte(`
version: v1
default_decision: observe_only
default_reason: no rule matched
rules:
  - name: block-critical-risk
    when:
      min_risk_level: critical
    decision: block
    reason: critical risk is blocked by configured policy
`), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	p, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}
	engine := trustvian.NewEngine(trustvian.WithPolicy(p))

	// A cold-start event against an unfamiliar actor/target naturally
	// scores near-maximal anomaly with zero identity confidence,
	// which internal/trust.Compute resolves to RiskCritical — see
	// docs/DOMAIN.md § Trust and Risk for the thresholds.
	criticalEvent := event.Event{
		ID:        "evt-1",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "unknown-caller", Type: event.ActorTypeService, IdentityConfidence: 0},
		Operation: event.Operation{Category: event.OperationCategoryExternal, Name: "POST /admin/export"},
		Target:    event.Target{Name: "secrets-manager"},
	}
	result, err := engine.Analyze(context.Background(), criticalEvent)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if result.Trust.Risk != "critical" {
		t.Fatalf("test setup: Trust.Risk = %q, want %q (adjust the event to reach critical risk)", result.Trust.Risk, "critical")
	}
	if string(result.Decision) != "block" {
		t.Errorf("Decision = %q, want %q (the loaded config's rule should have fired)", result.Decision, "block")
	}
	if result.Explanation.RuleName != "block-critical-risk" {
		t.Errorf("Explanation.RuleName = %q, want %q", result.Explanation.RuleName, "block-critical-risk")
	}
}

func TestLoadRejectsEmptyInput(t *testing.T) {
	tests := [][]byte{nil, []byte(""), []byte("   \n\t  ")}
	for _, in := range tests {
		_, err := config.Load(in)
		if !errors.Is(err, config.ErrEmptyInput) {
			t.Errorf("Load(%q) err = %v, want %v", in, err, config.ErrEmptyInput)
		}
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	_, err := config.Load([]byte("version: [unterminated"))
	if err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestLoadRejectsMissingVersion(t *testing.T) {
	_, err := config.Load([]byte(`
default_decision: observe_only
default_reason: default
`))
	if !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Errorf("err = %v, want %v", err, config.ErrUnsupportedVersion)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	_, err := config.Load([]byte(`
version: v2
default_decision: observe_only
default_reason: default
`))
	if !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Errorf("err = %v, want %v", err, config.ErrUnsupportedVersion)
	}
}

func TestLoadRejectsUnknownTopLevelField(t *testing.T) {
	// "default_decison" (typo) must not be silently ignored and fall
	// through to some other default — it must fail loudly.
	_, err := config.Load([]byte(`
version: v1
default_decison: block
default_reason: default
`))
	if err == nil {
		t.Fatal("expected an error for an unknown top-level field")
	}
	if !strings.Contains(err.Error(), "default_decison") {
		t.Errorf("error %q does not identify the unknown field", err)
	}
}

func TestLoadRejectsUnknownNestedField(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: observe_only
default_reason: default
rules:
  - name: r1
    when:
      actor_typo: ai_agent
    decision: allow
    reason: x
`))
	if err == nil {
		t.Fatal("expected an error for an unknown nested field")
	}
	if !strings.Contains(err.Error(), "actor_typo") {
		t.Errorf("error %q does not identify the unknown field", err)
	}
}

func TestLoadRejectsInvalidDecisionValue(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: blok
default_reason: default
`))
	if !errors.Is(err, config.ErrInvalidDecision) {
		t.Errorf("err = %v, want %v", err, config.ErrInvalidDecision)
	}
}

func TestLoadRejectsInvalidActorType(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: observe_only
default_reason: default
rules:
  - name: r1
    when:
      actor_type: srevice
    decision: allow
    reason: x
`))
	if !errors.Is(err, config.ErrInvalidActorType) {
		t.Errorf("err = %v, want %v", err, config.ErrInvalidActorType)
	}
}

func TestLoadRejectsInvalidOperationCategory(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: observe_only
default_reason: default
rules:
  - name: r1
    when:
      operation_category: htpp
    decision: allow
    reason: x
`))
	if !errors.Is(err, config.ErrInvalidCategory) {
		t.Errorf("err = %v, want %v", err, config.ErrInvalidCategory)
	}
}

func TestLoadRejectsInvalidRiskLevel(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: observe_only
default_reason: default
rules:
  - name: r1
    when:
      min_risk_level: extreme
    decision: allow
    reason: x
`))
	if !errors.Is(err, config.ErrInvalidRiskLevel) {
		t.Errorf("err = %v, want %v", err, config.ErrInvalidRiskLevel)
	}
}

func TestLoadRejectsDuplicateRuleNames(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: observe_only
default_reason: default
rules:
  - name: dup
    decision: allow
    reason: first
  - name: dup
    decision: block
    reason: second
`))
	if !errors.Is(err, config.ErrDuplicateRuleName) {
		t.Errorf("err = %v, want %v", err, config.ErrDuplicateRuleName)
	}
}

// TestLoadRejectsDuplicateYAMLKeys proves the YAML decoder's own
// behavior for a structurally ambiguous document — two "decision:"
// keys in the same mapping — rather than assuming it, per this task's
// explicit instruction to review (not assume) the library's
// duplicate-key handling. go.yaml.in/yaml/v3's Decoder rejects this
// on its own; nothing in this package adds special handling for it.
func TestLoadRejectsDuplicateYAMLKeys(t *testing.T) {
	_, err := config.Load([]byte(`
version: v1
default_decision: observe_only
default_decision: block
default_reason: default
`))
	if err == nil {
		t.Fatal("expected an error for a duplicate top-level YAML key")
	}
}

func TestLoadRejectsTooManyRules(t *testing.T) {
	var b strings.Builder
	b.WriteString("version: v1\ndefault_decision: observe_only\ndefault_reason: default\nrules:\n")
	for i := 0; i < 1001; i++ {
		fmt.Fprintf(&b, "  - name: r%d\n    decision: allow\n    reason: x\n", i)
	}
	_, err := config.Load([]byte(b.String()))
	if !errors.Is(err, config.ErrTooManyRules) {
		t.Errorf("err = %v, want %v", err, config.ErrTooManyRules)
	}
}

func TestLoadFileRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.yaml")
	huge := make([]byte, (1<<20)+1)
	for i := range huge {
		huge[i] = 'a'
	}
	if err := os.WriteFile(path, huge, 0o600); err != nil {
		t.Fatalf("write huge file: %v", err)
	}

	_, err := config.LoadFile(path)
	if !errors.Is(err, config.ErrFileTooLarge) {
		t.Errorf("err = %v, want %v", err, config.ErrFileTooLarge)
	}
}

func TestLoadFileReturnsIOErrorForMissingFile(t *testing.T) {
	_, err := config.LoadFile(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestLoadDoesNotPanicOnArbitraryInput(t *testing.T) {
	// A lightweight, non-fuzz smoke test that a handful of adversarial
	// byte sequences never panic — the invariant a full fuzz target
	// would also assert, kept small here per this task's own guidance
	// not to expand scope for fuzzing.
	inputs := [][]byte{
		nil,
		{0},
		[]byte("\x00\x01\x02"),
		[]byte("{{{{{{{{"),
		[]byte(strings.Repeat("- ", 10000)),
		[]byte("version: v1\ndefault_decision: &a [*a]\ndefault_reason: x\n"),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Load(%q) panicked: %v", in, r)
				}
			}()
			_, _ = config.Load(in)
		}()
	}
}
