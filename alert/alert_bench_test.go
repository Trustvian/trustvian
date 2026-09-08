package alert_test

import (
	"encoding/json"
	"testing"

	"github.com/Trustvian/trustvian/alert"
	"github.com/Trustvian/trustvian/internal/policy"
)

// BenchmarkEvaluateNoMatch is the common case: a Result that matches no
// configured Rule. Per docs/tasks/018-alert-notification-foundation.md
// § Performance, this path must stay allocation-conscious — no JSON
// marshaling and no network call ever happen on this path (Evaluate
// only builds an Alert, and only json.Marshal's cost, when a Rule
// actually matches).
func BenchmarkEvaluateNoMatch(b *testing.B) {
	result := sampleResult() // Decision: BLOCK
	rules := []alert.Rule{
		{When: alert.Condition{Decision: policy.DecisionAllow}, Severity: alert.SeverityInfo},
	}

	b.ReportAllocs()
	for b.Loop() {
		_, _ = alert.Evaluate(result, rules)
	}
}

// BenchmarkEvaluateMatch is the worst case for Evaluate itself: a
// matching Rule, which pays New's cost (id generation, Reasons
// construction) — but still no JSON marshaling, since Evaluate never
// serializes an Alert; only a Sink's Send does that, and only once
// delivery actually occurs.
func BenchmarkEvaluateMatch(b *testing.B) {
	result := sampleResult()
	rules := []alert.Rule{
		{When: alert.Condition{Decision: policy.DecisionBlock}, Severity: alert.SeverityCritical},
	}

	b.ReportAllocs()
	for b.Loop() {
		_, _ = alert.Evaluate(result, rules)
	}
}

// BenchmarkNewEnvelopeMarshal measures the payload-construction cost a
// Sink actually pays on delivery — kept in a separate benchmark from
// Evaluate's own, since that cost must never be paid on the no-match
// path (see BenchmarkEvaluateNoMatch's doc comment).
func BenchmarkNewEnvelopeMarshal(b *testing.B) {
	a := alert.New(sampleResult(), alert.SeverityCritical)

	b.ReportAllocs()
	for b.Loop() {
		_, err := json.Marshal(alert.NewEnvelope(a))
		if err != nil {
			b.Fatal(err)
		}
	}
}
