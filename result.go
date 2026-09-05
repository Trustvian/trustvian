package trustvian

import (
	"fmt"
	"strings"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

// Result is the outcome of Engine.Analyze: the output of every pipeline
// stage, retained so a Decision can always be explained, not just stated.
//
// Result's stage fields (Features, Fingerprint, Anomaly, Trust) are of
// types defined in this module's internal packages. That does not stop
// external callers from reading them — result.Trust.Score,
// result.Anomaly.Contributors, result.Decision and so on all work fine
// without importing anything beyond this root package — it only means an
// external caller cannot declare a variable of those types directly or
// construct one themselves. See Engine's doc comment for why
// configuration types (Policy, Config) carry the same restriction today.
type Result struct {
	// Event is the input this Result was computed from, retained for
	// audit/traceability.
	Event event.Event

	Features    features.Features
	Fingerprint fingerprint.Fingerprint
	BaselineKey baseline.Key
	Anomaly     anomaly.Anomaly
	Trust       trust.Trust

	Decision    policy.Decision
	Explanation policy.Explanation
}

// Explain renders r as a multi-line, human-readable summary of the full
// decision: what was decided, the trust/risk/anomaly scores, which
// signals contributed, and which policy rule (or default) produced it.
// It is pure formatting over r's existing fields — no new computation.
func (r Result) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Decision: %s\n", r.Decision)
	fmt.Fprintf(&b, "%s\n", r.Trust.Explain())
	fmt.Fprintf(&b, "Anomaly score: %.2f (confidence %.2f)\n", r.Anomaly.Score, r.Anomaly.Confidence)
	if len(r.Anomaly.Contributors) > 0 {
		b.WriteString("Detected:\n")
		for _, c := range r.Anomaly.Contributors {
			fmt.Fprintf(&b, "  - %s: %.2f", c.Name, c.Value)
			if c.Detail != "" {
				fmt.Fprintf(&b, " (%s)", c.Detail)
			}
			b.WriteString("\n")
		}
	}
	if r.Explanation.MatchedDefault {
		fmt.Fprintf(&b, "Policy: default action (%s)\n", r.Explanation.Reason)
	} else {
		fmt.Fprintf(&b, "Policy: rule %q (%s)\n", r.Explanation.RuleName, r.Explanation.Reason)
	}
	return b.String()
}
