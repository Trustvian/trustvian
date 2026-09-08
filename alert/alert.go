// Package alert turns a Trustvian Result/Decision into a minimal,
// explainable, externally deliverable notification — the "Foundation"
// stage of the Alert & Notification phase described in
// trustvian-project-spec.md § 18 and docs/tasks/018-alert-notification-foundation.md.
//
// This package is strictly downstream of the core detection pipeline:
// Alert is assembled by reading an existing trustvian.Result, never by
// adding a new pipeline stage. Nothing in this package writes back into
// a Result, and nothing upstream of Decision (event, internal/features
// through internal/policy, or Engine) imports this package or knows it
// exists. See docs/ARCHITECTURE.md § Relationship to a future Alert &
// Notification layer.
//
// Alert and Sink are exported from a package outside internal/ (unlike
// policy.Policy or anomaly.Config) because Sink's entire purpose is for
// code this module does not control to implement it against a stable
// Alert value — Go's internal/ visibility rule would make that
// impossible if Alert lived under internal/. See ADR 0007 for the full
// reasoning behind this package's location.
package alert

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

// Severity is notification urgency, distinct from every other Trustvian
// output value: severity != risk, severity != anomaly score, severity
// != trust score, severity != decision. A BLOCK at RiskCritical against
// a known-sensitive target might always be CRITICAL severity by an
// operator's own Rule; a CHALLENGE at RiskMedium on a first-time
// integration might reasonably be INFO. Severity is never derived by an
// implicit mapping from those other values — it is always exactly what
// the matched Rule configured (see Evaluate) — so there is no hidden
// mapping to audit.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Valid reports whether s is one of the five recognized Severity values.
func (s Severity) Valid() bool {
	switch s {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

// Alert is the notification-worthy summary of one behavioral decision —
// a view assembled from an existing trustvian.Result, not a parallel
// model that could drift from it. Every field below is read from a
// Result field that already exists; nothing here is a second, competing
// copy of trust score, anomaly score, risk, decision, actor, target, or
// explanation logic. See docs/tasks/018-alert-notification-foundation.md
// § Domain Model for the concept list this type implements.
type Alert struct {
	// ID identifies this Alert, distinct from Fingerprint.ID and from
	// Event.ID — an Alert is a new thing produced by evaluation, not a
	// renamed existing identifier.
	ID string `json:"id"`

	// Timestamp is the underlying Event's own timestamp (Result.Event.Timestamp),
	// not the moment Evaluate ran. This keeps New/Evaluate free of any
	// wall-clock dependency, which is also what makes payload
	// determinism testable: the same Result always produces the same
	// Alert.Timestamp.
	Timestamp time.Time `json:"timestamp"`

	Severity Severity        `json:"severity"`
	Decision policy.Decision `json:"decision"`
	Risk     trust.RiskLevel `json:"risk"`

	TrustScore   float64 `json:"trust_score"`
	AnomalyScore float64 `json:"anomaly_score"`

	Actor  event.Actor  `json:"actor"`
	Target event.Target `json:"target,omitzero"`

	FingerprintID string `json:"fingerprint_id"`

	// Reasons is built from Result.Anomaly.Contributors and
	// Result.Explanation — the same material Result.Explain() already
	// renders — not a new explanation engine.
	Reasons []string `json:"reasons,omitempty"`

	// Metadata is open, caller-populated extension data, matching
	// event.Event.Attributes's existing "open map, caller decides what
	// goes in it" precedent. Evaluate never populates this itself.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// New builds an Alert from result at the given severity. Most callers
// should use Evaluate instead, which also decides whether an Alert
// should exist at all; New is exposed directly for callers with their
// own severity-assignment logic that still want Alert's standard field
// derivation (reasons, actor/target, id generation) rather than
// reimplementing it.
func New(result trustvian.Result, severity Severity) Alert {
	return Alert{
		ID:            newRandomID("alt"),
		Timestamp:     result.Event.Timestamp,
		Severity:      severity,
		Decision:      result.Decision,
		Risk:          result.Trust.Risk,
		TrustScore:    result.Trust.Score,
		AnomalyScore:  result.Anomaly.Score,
		Actor:         result.Event.Actor,
		Target:        result.Event.Target,
		FingerprintID: result.Fingerprint.ID,
		Reasons:       reasonsFromResult(result),
	}
}

// reasonsFromResult renders Result.Anomaly.Contributors and
// Result.Explanation into a flat list of human-readable strings — the
// same material Result.Explain() already formats, reused rather than
// reimplemented as a second explanation engine.
func reasonsFromResult(result trustvian.Result) []string {
	reasons := make([]string, 0, len(result.Anomaly.Contributors)+1)
	for _, c := range result.Anomaly.Contributors {
		if c.Detail != "" {
			reasons = append(reasons, c.Detail)
		} else {
			reasons = append(reasons, c.Name)
		}
	}
	if result.Explanation.MatchedDefault {
		reasons = append(reasons, fmt.Sprintf("policy default: %s", result.Explanation.Reason))
	} else {
		reasons = append(reasons, fmt.Sprintf("policy rule %q: %s", result.Explanation.RuleName, result.Explanation.Reason))
	}
	return reasons
}

// PayloadVersion is the current version of the webhook payload contract
// (see Envelope). Bump this string for any future breaking change to
// Alert's wire shape — the same "no silent reinterpretation" discipline
// internal/fingerprint's versioned hash already established for this
// codebase.
const PayloadVersion = "1"

// Envelope is the versioned wire contract a Sink serializes: a stable
// top-level version field alongside the Alert itself, so a receiver can
// branch on payload shape without an out-of-band contract.
type Envelope struct {
	Version string `json:"version"`
	Alert   Alert  `json:"alert"`
}

// NewEnvelope wraps a at the current PayloadVersion.
func NewEnvelope(a Alert) Envelope {
	return Envelope{Version: PayloadVersion, Alert: a}
}

// Sink is the boundary every notification provider implements —
// conceptually named AlertSink in trustvian-project-spec.md § 18.6;
// named Sink here to avoid the package/type stutter (alert.Sink) other
// packages in this codebase generally avoid for secondary types. A new
// provider is added by implementing this one method against Alert's
// stable shape, without anything upstream of it changing. WebhookSink
// (webhook.go) is this stage's one implementation.
type Sink interface {
	Send(ctx context.Context, a Alert) error
}

// newRandomID generates a short, unique identifier with the given
// prefix, using crypto/rand rather than a package-level counter — this
// codebase avoids package-level mutable state (.claude/rules/go.md), so
// a global atomic counter is not an option here. A crypto/rand.Read
// failure is exceptionally rare (a broken OS entropy source) and is
// tolerated the same way the Go standard library's own internal users
// of crypto/rand typically do: the all-zero fallback is still a valid,
// merely non-random value, not a construction failure that would need
// to propagate through New/Evaluate's otherwise infallible signatures.
func newRandomID(prefix string) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return prefix + "_" + hex.EncodeToString(buf)
}
