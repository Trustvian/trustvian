// Package features extracts behavioral signals from an Event, split into
// stable dimensions (which aggregate into a Fingerprint) and volatile
// dimensions (which feed anomaly detection directly). See the event
// package for the input shape and internal/fingerprint for how StableFeatures
// accumulate into an actor's behavioral identity.
package features

import (
	"time"

	"github.com/Trustvian/trustvian/event"
)

// Attribute keys Extract looks for on Event.Attributes to derive volatile
// features. These are the conventions producers — including the future
// OTel adapter — are expected to populate.
const (
	// AttrDurationMS is the operation duration in milliseconds, as a
	// float64, float32, int, int32, or int64.
	AttrDurationMS = "duration_ms"
	// AttrError indicates the operation resulted in an error, as a bool.
	AttrError = "error"
)

// StableFeatures are the identity-defining dimensions of an Event: the
// characteristics that, aggregated across many events for the same actor,
// form a behavioral Fingerprint. They change slowly relative to
// VolatileFeatures.
type StableFeatures struct {
	ActorType         event.ActorType
	OperationCategory event.OperationCategory
	OperationName     string
	TargetName        string
	TargetCategory    event.TargetCategory
	Environment       string
}

// VolatileFeatures are the per-event, noisy dimensions of an Event: signals
// that feed anomaly detection directly but never join a stable
// Fingerprint.
type VolatileFeatures struct {
	Timestamp time.Time
	// Latency is the operation duration. Only meaningful when HasLatency
	// is true — its absence is distinct from a measured zero duration.
	Latency    time.Duration
	HasLatency bool
	Error      bool
}

// Features is the result of extracting both stable and volatile signals
// from a single Event.
type Features struct {
	Stable   StableFeatures
	Volatile VolatileFeatures
}

// Extract derives Features from e. It is a pure function of e: identical
// input always produces identical output, and e is never modified.
func Extract(e event.Event) Features {
	latency, hasLatency := durationMillisAttr(e.Attributes, AttrDurationMS)
	return Features{
		Stable: StableFeatures{
			ActorType:         e.Actor.Type,
			OperationCategory: e.Operation.Category,
			OperationName:     e.Operation.Name,
			TargetName:        e.Target.Name,
			TargetCategory:    e.Target.Category,
			Environment:       e.Context.Environment,
		},
		Volatile: VolatileFeatures{
			Timestamp:  e.Timestamp,
			Latency:    latency,
			HasLatency: hasLatency,
			Error:      boolAttr(e.Attributes, AttrError),
		},
	}
}

// durationMillisAttr reads a millisecond-valued numeric attribute and
// converts it to a time.Duration. It reports false if the key is absent or
// not a supported numeric type.
func durationMillisAttr(attrs map[string]any, key string) (time.Duration, bool) {
	v, ok := attrs[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return time.Duration(n * float64(time.Millisecond)), true
	case float32:
		return time.Duration(float64(n) * float64(time.Millisecond)), true
	case int:
		return time.Duration(n) * time.Millisecond, true
	case int32:
		return time.Duration(n) * time.Millisecond, true
	case int64:
		return time.Duration(n) * time.Millisecond, true
	default:
		return 0, false
	}
}

// boolAttr reads a bool-valued attribute, defaulting to false if the key
// is absent or not a bool.
func boolAttr(attrs map[string]any, key string) bool {
	v, ok := attrs[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}
