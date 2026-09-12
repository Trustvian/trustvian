package trustvian_test

// --- Task 032: Agent Security Scenario Validation ---
//
// This file validates that realistic AI-agent security scenarios can be
// expressed by *composing* capabilities the engine already has —
// categorical/fingerprint novelty (v0.1), sequence/transition/n-gram
// detection (v0.6), approval-aware policy (task 030), and delegation
// behavioral evidence (task 031) — through the ordinary, unmodified
// Analyze/Observe pipeline. No new anomaly signal, Policy primitive, or
// pipeline stage is introduced here; every helper below
// (agentEvent/toolEvent/riskGatedPolicy/approvalGatedPolicy/
// delegationGatedConfig/hasResultSignal) is inherited from
// engine_test.go's own task 014/030/031 sections. See
// docs/tasks/032-agent-security-scenario-validation.md and this
// module's ADRs 0014-0016 for the design each scenario below exercises.
//
// Every scenario test asserts on the *explainable* surface a security
// operator would actually read — Anomaly.Contributors, Trust,
// Explanation, and, where relevant, whether alert.Evaluate would
// produce an Alert — not on a bare numeric score in isolation.

import (
	"context"
	"fmt"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/store"
)

// destinationEvent builds a tool-call event with an explicit
// Target.Category, for scenario 5 (external destination drift) — the
// one scenario the other agent-shaped helpers in engine_test.go don't
// already parameterize.
func destinationEvent(actorID, targetName string, category event.TargetCategory, ts time.Time) event.Event {
	return event.Event{
		ID:        actorID + "-" + targetName + "-" + ts.String(),
		Timestamp: ts,
		Actor:     event.Actor{ID: actorID, Type: event.ActorTypeAIAgent, IdentityConfidence: 0.9},
		Operation: event.Operation{Category: event.OperationCategoryTool, Name: "fetch"},
		Target:    event.Target{Name: targetName, Category: category},
		Context:   event.Context{Environment: "production"},
	}
}

// TestScenarioUnexpectedPrivilegedTool is Task 032's Scenario 1.
//
// Normal: search/read/summarize. Observed: shell.execute. Expected:
// the *existing* categorical_novelty/transition_deviation signals fire
// with no agent-specific detector, and the full Trust/Explanation
// surface is readable — not just a bare score.
func TestScenarioUnexpectedPrivilegedTool(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	for i := range 20 {
		for _, tool := range []string{"search", "read", "summarize"} {
			result, err := engine.Analyze(ctx, agentEvent("agent-1", fmt.Sprintf("session-%d", i), tool, step()))
			if err != nil {
				t.Fatalf("Analyze(%s) error = %v", tool, err)
			}
			if _, err := engine.Observe(ctx, result); err != nil {
				t.Fatalf("Observe(%s) error = %v", tool, err)
			}
		}
	}

	privileged, err := engine.Analyze(ctx, agentEvent("agent-1", "session-attack", "shell.execute", step()))
	if err != nil {
		t.Fatalf("Analyze(shell.execute) error = %v", err)
	}

	if !hasResultSignal(privileged, "categorical_novelty") {
		t.Fatalf("Contributors = %+v, want categorical_novelty", privileged.Anomaly.Contributors)
	}
	if !hasResultSignal(privileged, "transition_deviation") || privileged.Anomaly.Contributors == nil {
		t.Errorf("Contributors = %+v, want transition_deviation too (no predecessor has ever led here)", privileged.Anomaly.Contributors)
	}
	// Explainability: an operator must be able to see *why*, not just
	// a number.
	if privileged.Explanation.Reason == "" {
		t.Errorf("Explanation.Reason is empty — a security decision must be explainable")
	}
	if privileged.Trust.Explain() == "" {
		t.Errorf("Trust.Explain() is empty")
	}
	for _, c := range privileged.Anomaly.Contributors {
		if c.Value > 0 && c.Detail == "" {
			t.Errorf("Contributor %q has Value=%v but no Detail", c.Name, c.Value)
		}
	}
}

// TestScenarioSensitiveExfiltrationSequence is Task 032's Scenario 2 —
// the mandatory proof that v0.6's existing bounded 3-gram detector,
// built with zero knowledge of AI agents or exfiltration, is what
// actually detects a sensitive-read-then-external-post sequence. No
// exfiltration-specific detector exists or is added.
func TestScenarioSensitiveExfiltrationSequence(t *testing.T) {
	ctx := context.Background()
	sharedStore := store.NewInMemory()
	warmupEngine := trustvian.NewEngine(trustvian.WithStore(sharedStore), trustvian.WithPolicy(riskGatedPolicy()))
	scoredCfg := anomaly.DefaultConfig()
	scoredCfg.NGramWeight = 0.9
	engine := trustvian.NewEngine(
		trustvian.WithStore(sharedStore),
		trustvian.WithPolicy(riskGatedPolicy()),
		trustvian.WithAnomalyConfig(scoredCfg),
	)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	analyzeAndObserve := func(sessionID, tool string, ts time.Time) trustvian.Result {
		t.Helper()
		result, err := warmupEngine.Analyze(ctx, agentEvent("agent-2", sessionID, tool, ts))
		if err != nil {
			t.Fatalf("Analyze(%s) error = %v", tool, err)
		}
		if _, err := warmupEngine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe(%s) error = %v", tool, err)
		}
		return result
	}

	// Actual normal path: search -> read -> summarize, repeated.
	for i := range 20 {
		s := fmt.Sprintf("normal-%d", i)
		analyzeAndObserve(s, "search", step())
		analyzeAndObserve(s, "read", step())
		analyzeAndObserve(s, "summarize", step())
	}
	// Familiarize search -> secret.read via a separate context.
	for i := range 20 {
		s := fmt.Sprintf("familiar-read-%d", i)
		analyzeAndObserve(s, "search", step())
		analyzeAndObserve(s, "secret.read", step())
		analyzeAndObserve(s, "audit_log", step())
	}
	// Familiarize secret.read -> external.post via yet another context.
	for i := range 20 {
		s := fmt.Sprintf("familiar-post-%d", i)
		analyzeAndObserve(s, "notify", step())
		analyzeAndObserve(s, "secret.read", step())
		analyzeAndObserve(s, "external.post", step())
	}

	// Position the 2-element history window at (search, secret.read),
	// then evaluate external.post as the never-before-seen 3-gram
	// completion.
	analyzeAndObserve("attack", "search", step())
	analyzeAndObserve("attack", "secret.read", step())

	exfil, err := engine.Analyze(ctx, agentEvent("agent-2", "attack", "external.post", step()))
	if err != nil {
		t.Fatalf("Analyze(external.post) error = %v", err)
	}

	if !hasResultSignal(exfil, "ngram_deviation") {
		t.Fatalf("Contributors = %+v, want ngram_deviation — search->secret.read->external.post has never been observed as one continuous 3-gram, despite both pairwise hops being familiar", exfil.Anomaly.Contributors)
	}
	if exfil.Explanation.Reason == "" {
		t.Errorf("Explanation.Reason is empty")
	}
}

// TestScenarioApprovalViolation is Task 032's Scenario 3: behaviorally
// familiar shell.execute, but Policy requires approval for it and the
// event's ApprovalStatus is not Approved. Approval failure is a Policy
// concern; Anomaly/Trust must remain unaffected.
func TestScenarioApprovalViolation(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(approvalGatedPolicy()))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	for range 30 {
		ev := toolEvent(event.ActorTypeAIAgent, "agent-3", "shell.execute", event.ApprovalApproved, step())
		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			t.Fatalf("Analyze (warm-up) error = %v", err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe (warm-up) error = %v", err)
		}
	}

	approved := toolEvent(event.ActorTypeAIAgent, "agent-3", "shell.execute", event.ApprovalApproved, step())
	allowed, err := engine.Analyze(ctx, approved)
	if err != nil {
		t.Fatalf("Analyze(Approved) error = %v", err)
	}

	denied := toolEvent(event.ActorTypeAIAgent, "agent-3", "shell.execute", event.ApprovalDenied, step())
	blocked, err := engine.Analyze(ctx, denied)
	if err != nil {
		t.Fatalf("Analyze(Denied) error = %v", err)
	}

	if allowed.Decision != policy.DecisionAllow {
		t.Errorf("Approved: Decision = %q, want %q", allowed.Decision, policy.DecisionAllow)
	}
	if blocked.Decision != policy.DecisionBlock {
		t.Errorf("Denied: Decision = %q, want %q — behaviorally familiar must not override missing approval", blocked.Decision, policy.DecisionBlock)
	}
	if allowed.Anomaly.Score != blocked.Anomaly.Score {
		t.Errorf("Anomaly.Score differs by ApprovalStatus alone: %v (Approved) vs %v (Denied) — approval is a Policy concern, not a behavioral one", allowed.Anomaly.Score, blocked.Anomaly.Score)
	}
	if allowed.Trust.Score != blocked.Trust.Score {
		t.Errorf("Trust.Score differs by ApprovalStatus alone: %v (Approved) vs %v (Denied)", allowed.Trust.Score, blocked.Trust.Score)
	}
	if blocked.Explanation.RuleName == "" || blocked.Explanation.Reason == "" {
		t.Errorf("Explanation = %+v, want a named rule and reason for the BLOCK", blocked.Explanation)
	}

	// Alert validation (§16): a security-relevant BLOCK should be
	// representable by the existing, unmodified alert package — no
	// ApprovalAlertEngine needed.
	rules := []alert.Rule{{Name: "approval-blocked", When: alert.Condition{Decision: policy.DecisionBlock}, Severity: alert.SeverityHigh}}
	if _, alerted := alert.Evaluate(blocked, rules); !alerted {
		t.Errorf("alert.Evaluate() alerted = false, want true for a BLOCKed approval violation")
	}
	if _, alerted := alert.Evaluate(allowed, rules); alerted {
		t.Errorf("alert.Evaluate() alerted = true for an ALLOWed, approved event, want false")
	}
}

// TestScenarioUnexpectedDelegator is Task 032's Scenario 4: a
// delegator this actor has never seen fires delegation_deviation, but
// — the mandatory counter-proof — that alone must not force BLOCK
// unless Policy independently chooses to act on the resulting risk.
// Delegation anomaly is evidence, not enforcement.
func TestScenarioUnexpectedDelegator(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	warmUp := func(engine *trustvian.Engine) {
		for i := range 20 {
			ev := agentEvent("agent-4", fmt.Sprintf("session-%d", i), "search", step())
			ev.Context.DelegatedFrom = "agent-a"
			result, err := engine.Analyze(ctx, ev)
			if err != nil {
				t.Fatalf("Analyze (warm-up) error = %v", err)
			}
			if _, err := engine.Observe(ctx, result); err != nil {
				t.Fatalf("Observe (warm-up) error = %v", err)
			}
		}
	}

	novelEvent := func() event.Event {
		ev := agentEvent("agent-4", "session-novel", "search", step())
		ev.Context.DelegatedFrom = "agent-x" // never observed for agent-4
		return ev
	}

	// Under a risk-gated policy, the elevated risk this signal
	// contributes CAN lead to BLOCK — but only because Policy chose to
	// gate on risk, not because delegation novelty is hardcoded to
	// block.
	riskGated := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(delegationGatedConfig()))
	warmUp(riskGated)
	underRiskPolicy, err := riskGated.Analyze(ctx, novelEvent())
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}
	if !hasResultSignal(underRiskPolicy, "delegation_deviation") {
		t.Fatalf("Contributors = %+v, want delegation_deviation", underRiskPolicy.Anomaly.Contributors)
	}

	// The mandatory counter-proof: a permissive policy with no
	// risk-based rule at all sees the identical anomaly evidence but
	// does NOT block — proving delegation_deviation is evidence
	// Policy may act on, never an enforcement mechanism in itself.
	permissive := policy.Policy{DefaultAction: policy.DecisionAllow, DefaultReason: "no rule gates on risk in this policy"}
	permissiveEngine := trustvian.NewEngine(trustvian.WithPolicy(permissive), trustvian.WithAnomalyConfig(delegationGatedConfig()))
	warmUp(permissiveEngine)
	underPermissivePolicy, err := permissiveEngine.Analyze(ctx, novelEvent())
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}
	if !hasResultSignal(underPermissivePolicy, "delegation_deviation") {
		t.Fatalf("Contributors = %+v, want delegation_deviation even under a permissive policy — the evidence itself must not depend on Policy", underPermissivePolicy.Anomaly.Contributors)
	}
	if underPermissivePolicy.Decision != policy.DecisionAllow {
		t.Errorf("Decision = %q under a permissive policy, want %q — unexpected delegation must not be an automatic BLOCK independent of Policy's own choice", underPermissivePolicy.Decision, policy.DecisionAllow)
	}
}

// TestScenarioExternalDestinationDrift is Task 032's Scenario 5: an
// agent's baseline matures against an internal destination, then
// reaches an external one — detected via the existing categorical
// novelty signal and Target.Category, with no agent-specific
// destination detector and no raw-URL fingerprinting.
func TestScenarioExternalDestinationDrift(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	for range 25 {
		result, err := engine.Analyze(ctx, destinationEvent("agent-5", "internal-crm", event.TargetCategoryInternal, step()))
		if err != nil {
			t.Fatalf("Analyze (warm-up) error = %v", err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe (warm-up) error = %v", err)
		}
	}

	drift, err := engine.Analyze(ctx, destinationEvent("agent-5", "external-webhook", event.TargetCategoryExternal, step()))
	if err != nil {
		t.Fatalf("Analyze(external) error = %v", err)
	}

	if !hasResultSignal(drift, "categorical_novelty") {
		t.Fatalf("Contributors = %+v, want categorical_novelty — a new TargetName/Category is a brand-new Fingerprint", drift.Anomaly.Contributors)
	}
	if drift.Event.Target.Category != event.TargetCategoryExternal {
		t.Fatalf("Target.Category = %q, want %q — normalized target/category semantics, not raw URL fingerprinting", drift.Event.Target.Category, event.TargetCategoryExternal)
	}
}

// TestScenarioCombinedDelegationSequenceApproval is Task 032's
// mandatory combined scenario (§11/§27 of the task brief): an
// unfamiliar delegator, a novel sensitive-then-external sequence, and
// missing approval, in one flow — proving the three evidence types
// (delegation, sequence, policy) stay orthogonal, explainable, and
// bounded rather than colliding or double-counting.
func TestScenarioCombinedDelegationSequenceApproval(t *testing.T) {
	ctx := context.Background()
	sharedStore := store.NewInMemory()

	// Approval requirement lives entirely in Policy — the identical
	// task-030 pattern, unmodified.
	approvalPolicy := policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "external-post-requires-approval",
				When:   policy.Condition{OperationCategory: event.OperationCategoryTool, TargetName: "external.post"},
				Unless: &policy.Condition{ApprovalStatus: event.ApprovalApproved},
				Action: policy.DecisionBlock,
				Reason: "external.post requires approval",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "no approval requirement configured for this operation",
	}

	cfg := anomaly.DefaultConfig()
	cfg.DelegationWeight = 0.7
	cfg.NGramWeight = 0.9

	warmupEngine := trustvian.NewEngine(trustvian.WithStore(sharedStore), trustvian.WithPolicy(approvalPolicy))
	engine := trustvian.NewEngine(trustvian.WithStore(sharedStore), trustvian.WithPolicy(approvalPolicy), trustvian.WithAnomalyConfig(cfg))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	agentEventWith := func(sessionID, tool, delegatedFrom string, approval event.ApprovalStatus, ts time.Time) event.Event {
		ev := agentEvent("agent-b", sessionID, tool, ts)
		ev.Context.DelegatedFrom = delegatedFrom
		ev.Context.ApprovalStatus = approval
		return ev
	}

	analyzeAndObserve := func(ev event.Event) trustvian.Result {
		t.Helper()
		result, err := warmupEngine.Analyze(ctx, ev)
		if err != nil {
			t.Fatalf("Analyze(%s) error = %v", ev.Operation.Name, err)
		}
		if _, err := warmupEngine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe(%s) error = %v", ev.Operation.Name, err)
		}
		return result
	}

	// agent-b's familiar delegator is agent-a, throughout every phase
	// below. agent-b's actual normal path.
	for i := range 20 {
		analyzeAndObserve(agentEventWith(fmt.Sprintf("normal-%d", i), "search", "agent-a", event.ApprovalApproved, step()))
	}
	// Familiarize search -> secret.read as a pairwise transition, via
	// search -> secret.read -> audit_log (never external.post) — the
	// identical technique
	// TestAnalyzeAgentToolSequenceNoveltyDetectedByExistingEngine (task
	// 014) already uses, and for the identical reason: without this
	// "safe" third step, this phase's own leftover 2-element history
	// window would silently teach the exact attack 3-gram this test
	// means to keep untrained.
	for i := range 20 {
		s := fmt.Sprintf("familiar-read-%d", i)
		analyzeAndObserve(agentEventWith(s, "search", "agent-a", event.ApprovalApproved, step()))
		analyzeAndObserve(agentEventWith(s, "secret.read", "agent-a", event.ApprovalApproved, step()))
		analyzeAndObserve(agentEventWith(s, "audit_log", "agent-a", event.ApprovalApproved, step()))
	}
	// Familiarize secret.read -> external.post as a pairwise
	// transition, via a *different* predecessor (notify, not search).
	for i := range 20 {
		s := fmt.Sprintf("familiar-post-%d", i)
		analyzeAndObserve(agentEventWith(s, "notify", "agent-a", event.ApprovalApproved, step()))
		analyzeAndObserve(agentEventWith(s, "secret.read", "agent-a", event.ApprovalApproved, step()))
		analyzeAndObserve(agentEventWith(s, "external.post", "agent-a", event.ApprovalApproved, step()))
	}
	// Position the 2-element history window at (search, secret.read) —
	// the attack sequence's first two steps, never trained as a
	// continuous path with external.post next.
	analyzeAndObserve(agentEventWith("attack", "search", "agent-a", event.ApprovalApproved, step()))
	analyzeAndObserve(agentEventWith("attack", "secret.read", "agent-a", event.ApprovalApproved, step()))

	// The combined attack: delegated from an unfamiliar agent, the
	// never-before-seen search->secret.read->external.post 3-gram, and
	// no approval.
	attack, err := engine.Analyze(ctx, agentEventWith("attack", "external.post", "agent-x", event.ApprovalUnspecified, step()))
	if err != nil {
		t.Fatalf("Analyze(attack) error = %v", err)
	}

	if !hasResultSignal(attack, "delegation_deviation") {
		t.Errorf("Contributors = %+v, want delegation_deviation (agent-x has never delegated to agent-b)", attack.Anomaly.Contributors)
	}
	if !hasResultSignal(attack, "ngram_deviation") {
		t.Errorf("Contributors = %+v, want ngram_deviation (search->secret.read->external.post never observed as one continuous sequence)", attack.Anomaly.Contributors)
	}
	if attack.Decision != policy.DecisionBlock {
		t.Errorf("Decision = %q, want %q — missing approval must block regardless of the behavioral evidence's own magnitude", attack.Decision, policy.DecisionBlock)
	}
	if attack.Explanation.RuleName != "external-post-requires-approval" {
		t.Errorf("Explanation.RuleName = %q, want %q — the decision must be traceable to the actual rule that fired", attack.Explanation.RuleName, "external-post-requires-approval")
	}
	// Bounded: combine()'s noisy-OR always returns [0,1] by
	// construction; this assertion exists so a future change that
	// breaks that invariant fails loudly here, not just in
	// internal/anomaly's own unit tests.
	if attack.Anomaly.Score < 0 || attack.Anomaly.Score > 1 {
		t.Errorf("Anomaly.Score = %v, want in [0,1]", attack.Anomaly.Score)
	}
	if attack.Trust.Score < 0 || attack.Trust.Score > 1 {
		t.Errorf("Trust.Score = %v, want in [0,1]", attack.Trust.Score)
	}

	// No correlated-evidence explosion (§12): delegation_deviation and
	// ngram_deviation measure genuinely independent dimensions (who
	// delegated vs. what sequence of operations) — noisy-OR combining
	// both is not double-counting, and no third, redundant signal
	// should appear for the identical evidence.
	seen := map[string]int{}
	for _, c := range attack.Anomaly.Contributors {
		if c.Value > 0 {
			seen[c.Name]++
		}
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("signal %q appears %d times in Contributors, want at most once", name, count)
		}
	}
}

// TestScenarioCombinedPoisoningRegression is Task 032's mandatory
// combined poisoning regression (§29 of the task brief): repeated
// BLOCKed attempts combining a novel delegator with a malicious tool
// sequence must never "normalize" through repetition — the existing,
// unmodified eligibleForLearning gate protects every dimension of
// combined evidence at once, with no separate AI-poisoning mechanism.
func TestScenarioCombinedPoisoningRegression(t *testing.T) {
	ctx := context.Background()
	approvalPolicy := policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "shell-execute-requires-approval",
				When:   policy.Condition{OperationCategory: event.OperationCategoryTool, TargetName: "shell.execute"},
				Unless: &policy.Condition{ApprovalStatus: event.ApprovalApproved},
				Action: policy.DecisionBlock,
				Reason: "shell.execute requires approval",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "no approval requirement configured",
	}
	cfg := anomaly.DefaultConfig()
	cfg.DelegationWeight = 0.7
	engine := trustvian.NewEngine(trustvian.WithPolicy(approvalPolicy), trustvian.WithAnomalyConfig(cfg))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	var lastResult trustvian.Result
	for range 30 {
		ev := agentEvent("agent-c", "session-attack", "shell.execute", step())
		ev.Context.DelegatedFrom = "agent-x" // never legitimately delegates to agent-c
		ev.Context.ApprovalStatus = event.ApprovalDenied
		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			t.Fatalf("Analyze error = %v", err)
		}
		if result.Decision != policy.DecisionBlock {
			t.Fatalf("Decision = %q, want %q — this test requires every attempt to be learning-ineligible", result.Decision, policy.DecisionBlock)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			t.Fatalf("Observe error = %v", err)
		}
		if learned {
			t.Fatalf("Observe() learned = true, want false for a BLOCKed decision")
		}
		lastResult = result
	}

	if !hasResultSignal(lastResult, "delegation_deviation") {
		t.Errorf("Contributors = %+v, want delegation_deviation to remain present after 30 repeated, ineligible attempts", lastResult.Anomaly.Contributors)
	}
	if lastResult.Decision != policy.DecisionBlock {
		t.Errorf("Decision = %q after 30 repeated attempts, want %q — the attack must never normalize", lastResult.Decision, policy.DecisionBlock)
	}
}

// TestScenarioIdentityConfidenceStaysIndependent is Task 032's
// mandatory identity-confidence-independence check (§14 of the task
// brief): for otherwise-identical events differing only in
// Actor.IdentityConfidence, delegation/approval evidence must be
// unaffected — IdentityConfidence influences only trust.Trust.Score's
// own multiplicative term, exactly as trust.Compute already documents,
// never delegation_deviation's or the approval rule's own evaluation.
func TestScenarioIdentityConfidenceStaysIndependent(t *testing.T) {
	ctx := context.Background()
	engine := trustvian.NewEngine(trustvian.WithPolicy(approvalGatedPolicy()), trustvian.WithAnomalyConfig(delegationGatedConfig()))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	highConfidence := agentEvent("agent-d", "session-1", "shell.execute", now)
	highConfidence.Actor.IdentityConfidence = 0.99
	highConfidence.Context.DelegatedFrom = "agent-x"
	highConfidence.Context.ApprovalStatus = event.ApprovalDenied

	lowConfidence := agentEvent("agent-d", "session-1", "shell.execute", now)
	lowConfidence.ID += "-low"
	lowConfidence.Actor.IdentityConfidence = 0.2
	lowConfidence.Context.DelegatedFrom = "agent-x"
	lowConfidence.Context.ApprovalStatus = event.ApprovalDenied

	high, err := engine.Analyze(ctx, highConfidence)
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}
	low, err := engine.Analyze(ctx, lowConfidence)
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}

	if high.Anomaly.Score != low.Anomaly.Score {
		t.Errorf("Anomaly.Score differs by IdentityConfidence alone: %v vs %v — delegation_deviation must not read identity confidence", high.Anomaly.Score, low.Anomaly.Score)
	}
	if high.Decision != policy.DecisionBlock || low.Decision != policy.DecisionBlock {
		t.Errorf("Decision = %q (high) / %q (low), want both %q — the approval requirement is independent of identity confidence", high.Decision, low.Decision, policy.DecisionBlock)
	}
	// IdentityConfidence's only documented effect is on Trust.Score's
	// own multiplicative term (trust.Compute), which this test
	// confirms differs — proving the field is *not* silently inert,
	// only that it doesn't leak into delegation or policy evaluation.
	if high.Trust.Score == low.Trust.Score {
		t.Errorf("Trust.Score identical despite different IdentityConfidence (%v (high) vs %v (low)) — IdentityConfidence should still influence Trust.Score", high.Trust.Score, low.Trust.Score)
	}
	if high.Trust.IdentityConfidence != 0.99 || low.Trust.IdentityConfidence != 0.2 {
		t.Errorf("Trust.IdentityConfidence = %v (high) / %v (low), want 0.99 / 0.2", high.Trust.IdentityConfidence, low.Trust.IdentityConfidence)
	}
}

// TestScenarioNoCorrelatedEvidenceExplosionUnderEveryWeight is Task
// 032's mandatory audit (§12 of the task brief): with every v0.6/v0.7
// signal weight enabled simultaneously (transition, transition
// rarity, n-gram, n-gram rarity, Markov, delegation), the existing
// mutual-exclusion protection (ADR 0013: transition_rarity is forced
// to Weight=0 whenever MarkovWeight>0) still holds, and the combined
// score stays within [0,1] — no AI-agent-specific scenario magnifies
// correlated evidence beyond what v0.6 itself already guarantees.
func TestScenarioNoCorrelatedEvidenceExplosionUnderEveryWeight(t *testing.T) {
	ctx := context.Background()
	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7
	cfg.TransitionRarityWeight = 0.7
	cfg.NGramWeight = 0.7
	cfg.NGramRarityWeight = 0.7
	cfg.MarkovWeight = 0.7
	cfg.DelegationWeight = 0.7
	engine := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(cfg))
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := func() time.Time { now = now.Add(time.Second); return now }

	for i := range 25 {
		ev := agentEvent("agent-e", fmt.Sprintf("session-%d", i), "search", step())
		ev.Context.DelegatedFrom = "agent-a"
		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			t.Fatalf("Analyze (warm-up) error = %v", err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe (warm-up) error = %v", err)
		}
	}

	attack := agentEvent("agent-e", "session-attack", "shell.execute", step())
	attack.Context.DelegatedFrom = "agent-x"
	result, err := engine.Analyze(ctx, attack)
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}

	if result.Anomaly.Score < 0 || result.Anomaly.Score > 1 {
		t.Fatalf("Anomaly.Score = %v, want in [0,1] even with every signal weight enabled at once", result.Anomaly.Score)
	}
	// transition_rarity/markov_surprisal remain mutually exclusive by
	// construction (see anomaly.Score's own MarkovWeight handling):
	// both are never simultaneously non-zero contributors.
	var transitionRarityValue, markovValue float64
	for _, c := range result.Anomaly.Contributors {
		switch c.Name {
		case "transition_rarity":
			transitionRarityValue = c.Value * c.Weight
		case "markov_surprisal":
			markovValue = c.Value * c.Weight
		}
	}
	if transitionRarityValue > 0 && markovValue > 0 {
		t.Errorf("both transition_rarity (%v) and markov_surprisal (%v) contributed simultaneously — ADR 0013's mutual-exclusion guarantee is violated", transitionRarityValue, markovValue)
	}
}

// TestScenarioSessionCardinalityRegression re-runs task 014's own
// mandatory 1,000-distinct-SessionID proof (§30 of the task brief) to
// confirm Task 032's scenario work did not accidentally cause
// SessionID to enter baseline/fingerprint identity.
func TestScenarioSessionCardinalityRegression(t *testing.T) {
	ctx := context.Background()
	scenarioStore := store.NewInMemory()
	engine := trustvian.NewEngine(trustvian.WithStore(scenarioStore))

	const observations = 1000
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var lastResult trustvian.Result
	for i := range observations {
		now = now.Add(time.Second)
		sessionID := fmt.Sprintf("session-%d", i)
		result, err := engine.Analyze(ctx, agentEvent("scenario-agent", sessionID, "search", now))
		if err != nil {
			t.Fatalf("Analyze() call %d: error = %v", i, err)
		}
		if _, err := engine.Observe(ctx, result); err != nil {
			t.Fatalf("Observe() call %d: error = %v", i, err)
		}
		lastResult = result
	}

	bl, ok := scenarioStore.Get(ctx, lastResult.BaselineKey)
	if !ok {
		t.Fatalf("Get(%+v) ok = false, want true", lastResult.BaselineKey)
	}
	if got := len(bl.Fingerprints); got != 1 {
		t.Fatalf("len(Baseline.Fingerprints) = %d, want 1 — 1,000 distinct SessionIDs must not create 1,000 distinct Fingerprints", got)
	}
}

// TestScenarioNonAgentRegressionUnaffected is Task 032's mandatory
// non-agent regression (§31 of the task brief): a plain service actor,
// with every scenario-relevant weight enabled, behaves exactly as a
// pre-v0.7 service actor would — no scenario work changes non-agent
// workloads.
func TestScenarioNonAgentRegressionUnaffected(t *testing.T) {
	ctx := context.Background()
	cfg := anomaly.DefaultConfig()
	cfg.DelegationWeight = 0.7 // enabled, but this event never carries DelegatedFrom
	withScenarioConfig := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy()), trustvian.WithAnomalyConfig(cfg))
	withDefaultConfig := trustvian.NewEngine(trustvian.WithPolicy(riskGatedPolicy())) // DelegationWeight defaults to 0
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	ev := paymentEventAt(10, "evt-1", now) // a plain service event, no agent context at all

	got, err := withScenarioConfig.Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}
	want, err := withDefaultConfig.Analyze(ctx, ev)
	if err != nil {
		t.Fatalf("Analyze error = %v", err)
	}

	if got.Anomaly.Score != want.Anomaly.Score || got.Decision != want.Decision {
		t.Errorf("Task 032's scenario config changed a non-agent event's result: Score %v->%v, Decision %q->%q", want.Anomaly.Score, got.Anomaly.Score, want.Decision, got.Decision)
	}
}
