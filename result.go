package trustvian

import (
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
