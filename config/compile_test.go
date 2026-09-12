package config_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
	"github.com/Trustvian/trustvian/event"
)

func TestCompilePolicyRejectsInvalidConfig(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Version = "bogus"

	_, err := config.CompilePolicy(cfg)
	if !errors.Is(err, config.ErrUnsupportedVersion) {
		t.Errorf("CompilePolicy() err = %v, want %v", err, config.ErrUnsupportedVersion)
	}
}

func TestCompilePolicyPreservesRuleOrder(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Rules = []config.PolicyRule{
		{Name: "first", Decision: "allow", Reason: "r1"},
		{Name: "second", Decision: "block", Reason: "r2"},
		{Name: "third", Decision: "alert", Reason: "r3"},
	}

	p, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}

	got := []string{p.Rules[0].Name, p.Rules[1].Name, p.Rules[2].Name}
	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("compiled rule order = %v, want %v", got, want)
	}
}

func TestCompilePolicyIsDeterministic(t *testing.T) {
	cfg := fullValidConfig()

	p1, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy (1st): %v", err)
	}
	p2, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy (2nd): %v", err)
	}

	if !reflect.DeepEqual(p1, p2) {
		t.Errorf("compiling the same PolicyConfig twice produced different results:\n%+v\nvs\n%+v", p1, p2)
	}
}

func TestCompilePolicyDoesNotMutateConfig(t *testing.T) {
	cfg := fullValidConfig()
	before := fullValidConfig()

	_, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}

	if !reflect.DeepEqual(cfg, before) {
		t.Errorf("CompilePolicy mutated its input:\ngot  %+v\nwant %+v", cfg, before)
	}
}

func TestCompileConditionTranslatesEveryField(t *testing.T) {
	cfg := config.PolicyConfig{
		Version:         config.SchemaVersionV1,
		DefaultDecision: "observe_only",
		DefaultReason:   "default",
		Rules: []config.PolicyRule{{
			Name: "r1",
			When: config.PolicyCondition{
				ActorType:         "ai_agent",
				OperationCategory: "tool",
				TargetName:        "secrets-manager",
				Environment:       "production",
				MinRiskLevel:      "high",
				Attributes:        map[string]string{"tool.category": "secrets"},
				ApprovalStatus:    "approved",
			},
			Decision: "block",
			Reason:   "test",
		}},
	}

	p, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}

	when := p.Rules[0].When
	if string(when.ActorType) != "ai_agent" {
		t.Errorf("ActorType = %q, want %q", when.ActorType, "ai_agent")
	}
	if string(when.OperationCategory) != "tool" {
		t.Errorf("OperationCategory = %q, want %q", when.OperationCategory, "tool")
	}
	if when.TargetName != "secrets-manager" {
		t.Errorf("TargetName = %q, want %q", when.TargetName, "secrets-manager")
	}
	if when.Environment != "production" {
		t.Errorf("Environment = %q, want %q", when.Environment, "production")
	}
	if string(when.MinRiskLevel) != "high" {
		t.Errorf("MinRiskLevel = %q, want %q", when.MinRiskLevel, "high")
	}
	if when.Attributes["tool.category"] != "secrets" {
		t.Errorf("Attributes[tool.category] = %q, want %q", when.Attributes["tool.category"], "secrets")
	}
	if string(when.ApprovalStatus) != "approved" {
		t.Errorf("ApprovalStatus = %q, want %q", when.ApprovalStatus, "approved")
	}
}

// TestCompileApprovalConditionViaUnless is task 030's end-to-end
// compile-level proof of the canonical "requires approval" pattern:
// When matches the operation, Unless matches ApprovalStatus: approved
// — CompilePolicy must translate both halves faithfully, since the
// Unless side is where the actual enforcement lives.
func TestCompileApprovalConditionViaUnless(t *testing.T) {
	cfg := config.PolicyConfig{
		Version:         config.SchemaVersionV1,
		DefaultDecision: "allow",
		DefaultReason:   "no approval requirement configured",
		Rules: []config.PolicyRule{{
			Name: "shell-execute-requires-approval",
			When: config.PolicyCondition{
				OperationCategory: "tool",
				TargetName:        "shell.execute",
			},
			Unless: &config.PolicyCondition{
				ApprovalStatus: "approved",
			},
			Decision: "block",
			Reason:   "shell.execute requires approval",
		}},
	}

	p, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}

	rule := p.Rules[0]
	if rule.Unless == nil {
		t.Fatalf("Unless = nil, want a compiled Condition")
	}
	if string(rule.Unless.ApprovalStatus) != "approved" {
		t.Errorf("Unless.ApprovalStatus = %q, want %q", rule.Unless.ApprovalStatus, "approved")
	}
}

// TestEndToEndConfiguredPolicyProducesConfiguredDecision is the
// runtime integration test this task's own acceptance criteria
// require: Config -> Validate -> Compile -> Engine -> Analyze ->
// Result -> a Decision the configured Policy, not the built-in
// default, actually produced. Proving only that PolicyConfig
// unmarshals or that CompilePolicy returns no error would not prove
// this — the point of v0.5 is real configurable behavior.
func TestEndToEndConfiguredPolicyProducesConfiguredDecision(t *testing.T) {
	cfg := config.PolicyConfig{
		Version:         config.SchemaVersionV1,
		DefaultDecision: "observe_only",
		DefaultReason:   "no rule matched",
		Rules: []config.PolicyRule{{
			Name: "block-agent-secrets",
			When: config.PolicyCondition{
				ActorType: "ai_agent",
				Attributes: map[string]string{
					"tool.category": "secrets",
				},
			},
			Decision: "block",
			Reason:   "AI agent secret access is blocked by configured policy",
		}},
	}

	p, err := config.CompilePolicy(cfg)
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}

	// The exact external-module usage this design exists to support:
	// p's static type is policy.Policy (internal to this module), but
	// this test file — like a genuinely separate module would — never
	// names that type; it only ever receives it from CompilePolicy and
	// passes it straight into trustvian.WithPolicy.
	engine := trustvian.NewEngine(trustvian.WithPolicy(p))

	blockedEvent := event.Event{
		ID:        "evt-1",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "support-agent", Type: event.ActorTypeAIAgent, IdentityConfidence: 0.9},
		Operation: event.Operation{Category: event.OperationCategoryTool, Name: "get_credentials"},
		Target:    event.Target{Name: "secrets-manager"},
		Attributes: map[string]any{
			"tool.category": "secrets",
		},
	}

	result, err := engine.Analyze(context.Background(), blockedEvent)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if string(result.Decision) != "block" {
		t.Errorf("Decision = %q, want %q (the configured rule should have fired)", result.Decision, "block")
	}
	if result.Explanation.RuleName != "block-agent-secrets" {
		t.Errorf("Explanation.RuleName = %q, want %q", result.Explanation.RuleName, "block-agent-secrets")
	}

	// A different event that the rule does NOT match must fall through
	// to the configured default, proving the rest of the Policy — not
	// just the one rule — is real too.
	allowedEvent := event.Event{
		ID:        "evt-2",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "svc-payment", Type: event.ActorTypeService, IdentityConfidence: 0.95},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /orders"},
		Target:    event.Target{Name: "orders-api"},
	}
	result2, err := engine.Analyze(context.Background(), allowedEvent)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if string(result2.Decision) != "observe_only" {
		t.Errorf("Decision = %q, want %q (default, unmatched by the configured rule)", result2.Decision, "observe_only")
	}
	if !result2.Explanation.MatchedDefault {
		t.Error("Explanation.MatchedDefault = false, want true")
	}
}

// TestCompiledPolicyIsSafeForConcurrentAnalyze proves the "compile
// once, read concurrently, no locks" property this task's Concurrency
// section requires: a policy.Policy compiled once via CompilePolicy is
// immutable data, so many goroutines calling Engine.Analyze against
// the same Engine (which embeds that Policy) concurrently must be
// race-clean — run this test with -race, which is what actually
// proves it.
func TestCompiledPolicyIsSafeForConcurrentAnalyze(t *testing.T) {
	p, err := config.CompilePolicy(fullValidConfig())
	if err != nil {
		t.Fatalf("CompilePolicy: %v", err)
	}
	engine := trustvian.NewEngine(trustvian.WithPolicy(p))

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := event.Event{
				ID:        "evt",
				Timestamp: time.Now(),
				Actor:     event.Actor{ID: "svc", Type: event.ActorTypeService, IdentityConfidence: 0.9},
				Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
			}
			if _, err := engine.Analyze(context.Background(), ev); err != nil {
				t.Errorf("goroutine %d: Analyze: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}
