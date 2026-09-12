// Command ai-agent-security ports docs/tasks/032-agent-security-scenario-validation.md's
// "familiar != authorized" scenario, through the real, declarative
// configuration path an OSS user would actually use: a config.PolicyConfig
// value (the same schema a trustvian.yaml file decodes into) requires
// approval for shell.execute, compiled with config.CompilePolicy into the
// policy.Policy trustvian.WithPolicy accepts — no internal/policy import
// anywhere in this file. An AI agent's shell.execute tool call matures
// into a fully familiar behavior (25 Analyze+Observe calls, all
// Approved), then the identical, now-familiar call is evaluated twice
// more: once Approved (allowed), once Denied (blocked) — proving that
// behavioral familiarity and policy authorization are different
// questions, entirely from outside this module. See README.md for real
// captured output, and its own "What this example does not show"
// section for the one piece of task 032's validation (delegation/
// sequence signals) that genuinely cannot be expressed through public
// API alone today.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/config"
	"github.com/Trustvian/trustvian/event"
)

func shellExecuteEvent(id string, approval event.ApprovalStatus, ts time.Time) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: ts,
		Actor: event.Actor{
			ID:                 "ops-agent",
			Type:               event.ActorTypeAIAgent,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryTool,
			Name:     "shell.execute",
		},
		Target:     event.Target{Name: "shell.execute"},
		Context:    event.Context{Environment: "production", ApprovalStatus: approval},
		Attributes: map[string]any{"duration_ms": 40},
	}
}

func main() {
	// The approval requirement lives entirely in declarative Policy
	// config — the same shape a trustvian.yaml file decodes into (see
	// docs/policy-guide.md § Loading a Policy from a YAML file). The
	// event never gets to declare its own requirement away: Unless
	// only exempts ApprovalStatus == "approved"; every other value,
	// including "not_required", still blocks.
	cfg := config.PolicyConfig{
		Version:         config.SchemaVersionV1,
		DefaultDecision: "allow",
		DefaultReason:   "no approval requirement configured for this operation",
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
			Reason:   "shell.execute requires approval; approval evidence was not Approved",
		}},
	}

	p, err := config.CompilePolicy(cfg)
	if err != nil {
		log.Fatalf("compile policy: %v", err)
	}

	engine := trustvian.NewEngine(trustvian.WithPolicy(p))
	ctx := context.Background()

	// A fixed clock, advancing at a steady one-minute cadence, keeps
	// this run deterministic and keeps frequency_deviation (opt-in,
	// zero weight by default, but still computed and reported) reading
	// as an unremarkable, steady cadence rather than tight-loop jitter
	// — the same reason examples/frequency-abuse uses a fixed clock,
	// applied here so this example's own printed output stays focused
	// on the approval narrative it's actually about.
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tick := func() time.Time { clock = clock.Add(time.Minute); return clock }

	// Mature this agent's shell.execute into fully familiar behavior —
	// every warm-up call is itself Approved, so nothing here is ever
	// blocked.
	for i := 1; i <= 25; i++ {
		result, err := engine.Analyze(ctx, shellExecuteEvent(fmt.Sprintf("warm-up-%d", i), event.ApprovalApproved, tick()))
		if err != nil {
			log.Fatalf("analyze warm-up %d: %v", i, err)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			log.Fatalf("observe warm-up %d: %v", i, err)
		}
		if i == 1 || i == 10 || i == 20 || i == 25 {
			fmt.Printf("warm-up %2d: confidence=%.2f trust=%.2f decision=%s learned=%v\n",
				i, result.Anomaly.Confidence, result.Trust.Score, result.Decision, learned)
		}
	}

	fmt.Println()
	fmt.Println("shell.execute is now fully familiar behavior for this agent.")
	fmt.Println()

	// Both cases below share the identical timestamp — Analyze is
	// read-only (only Observe would advance the learned baseline), so
	// evaluating both at the same steady-cadence tick keeps
	// frequency_deviation reading as unremarkable for both, isolating
	// the one variable this example is actually about: ApprovalStatus.
	evalTime := tick()

	approved, err := engine.Analyze(ctx, shellExecuteEvent("evt-approved", event.ApprovalApproved, evalTime))
	if err != nil {
		log.Fatalf("analyze approved: %v", err)
	}
	fmt.Println("Case A — ApprovalStatus: approved")
	fmt.Println(approved.Explain())
	fmt.Printf("Decision: %s (rule %q: %s)\n\n", approved.Decision, approved.Explanation.RuleName, approved.Explanation.Reason)

	denied, err := engine.Analyze(ctx, shellExecuteEvent("evt-denied", event.ApprovalDenied, evalTime))
	if err != nil {
		log.Fatalf("analyze denied: %v", err)
	}
	fmt.Println("Case B — ApprovalStatus: denied (same familiar behavior)")
	fmt.Println(denied.Explain())
	fmt.Printf("Decision: %s (rule %q: %s)\n\n", denied.Decision, denied.Explanation.RuleName, denied.Explanation.Reason)

	// Alert Validation: the existing, unmodified alert package
	// represents an approval-policy BLOCK exactly like any other
	// block — receiving denied.Decision (already typed policy.Decision
	// from a public trustvian.Result field) and passing it straight
	// into alert.Condition works via Go's normal type inference,
	// without this file ever importing internal/policy — the identical
	// trick config.CompilePolicy's own return value already relies on
	// (see docs/adr/0008-policy-config-boundary.md).
	rules := []alert.Rule{
		{Name: "approval-violation", When: alert.Condition{Decision: denied.Decision}, Severity: alert.SeverityHigh},
	}
	if a, ok := alert.Evaluate(denied, rules); ok {
		fmt.Printf("Alert: %s severity=%s reasons=%v\n", a.ID, a.Severity, a.Reasons)
	}
}
