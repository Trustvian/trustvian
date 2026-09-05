package fingerprint_test

import (
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

func stableFeatures(actorType event.ActorType, category event.OperationCategory, name, target, env string) features.StableFeatures {
	return features.StableFeatures{
		ActorType:         actorType,
		OperationCategory: category,
		OperationName:     name,
		TargetName:        target,
		Environment:       env,
	}
}

func TestComputeDeterministic(t *testing.T) {
	stable := stableFeatures(event.ActorTypeService, event.OperationCategoryHTTP, "POST /payment", "payment-db", "production")

	first := fingerprint.Compute(stable)
	second := fingerprint.Compute(stable)

	if first.ID != second.ID {
		t.Fatalf("Compute is not deterministic: first=%q second=%q", first.ID, second.ID)
	}
	if first.Stable != stable {
		t.Fatalf("Fingerprint.Stable = %+v, want %+v", first.Stable, stable)
	}
}

func TestComputeDiffersOnStableDimensions(t *testing.T) {
	base := stableFeatures(event.ActorTypeService, event.OperationCategoryHTTP, "POST /payment", "payment-db", "production")
	baseID := fingerprint.Compute(base).ID

	tests := []struct {
		name   string
		mutate func(features.StableFeatures) features.StableFeatures
	}{
		{"different actor type", func(s features.StableFeatures) features.StableFeatures {
			s.ActorType = event.ActorTypeAIAgent
			return s
		}},
		{"different operation category", func(s features.StableFeatures) features.StableFeatures {
			s.OperationCategory = event.OperationCategoryDB
			return s
		}},
		{"different operation name", func(s features.StableFeatures) features.StableFeatures { s.OperationName = "GET /payment"; return s }},
		{"different target", func(s features.StableFeatures) features.StableFeatures { s.TargetName = "admin-db"; return s }},
		{"different target category", func(s features.StableFeatures) features.StableFeatures {
			s.TargetCategory = event.TargetCategoryDatabase
			return s
		}},
		{"different environment", func(s features.StableFeatures) features.StableFeatures { s.Environment = "staging"; return s }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := fingerprint.Compute(tt.mutate(base)).ID
			if id == baseID {
				t.Fatalf("Compute() ID unchanged after mutating %s: %q", tt.name, id)
			}
		})
	}
}

func TestComputeAvoidsFieldBoundaryCollision(t *testing.T) {
	a := stableFeatures(event.ActorTypeService, event.OperationCategoryHTTP, "ab", "c", "production")
	b := stableFeatures(event.ActorTypeService, event.OperationCategoryHTTP, "a", "bc", "production")

	idA := fingerprint.Compute(a).ID
	idB := fingerprint.Compute(b).ID

	if idA == idB {
		t.Fatalf("Compute() collided across a field boundary shift: %q", idA)
	}
}

func TestComputeIgnoresVolatileChanges(t *testing.T) {
	mkEvent := func(latencyMS float64, hasError bool, ts time.Time) event.Event {
		return event.Event{
			ID:        "evt-1",
			Timestamp: ts,
			Actor: event.Actor{
				ID:                 "svc-payment",
				Type:               event.ActorTypeService,
				IdentityConfidence: 0.9,
			},
			Operation: event.Operation{
				Category: event.OperationCategoryHTTP,
				Name:     "POST /payment",
			},
			Target:  event.Target{Name: "payment-db"},
			Context: event.Context{Environment: "production"},
			Attributes: map[string]any{
				features.AttrDurationMS: latencyMS,
				features.AttrError:      hasError,
			},
		}
	}

	e1 := mkEvent(10, false, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	e2 := mkEvent(9000, true, time.Date(2026, 1, 1, 13, 30, 0, 0, time.UTC))

	fp1 := fingerprint.Compute(features.Extract(e1).Stable)
	fp2 := fingerprint.Compute(features.Extract(e2).Stable)

	if fp1.ID != fp2.ID {
		t.Fatalf("Fingerprint ID changed despite only volatile fields differing: %q vs %q", fp1.ID, fp2.ID)
	}
}

func TestFingerprintIDIndependentOfEventIdentifiers(t *testing.T) {
	base := event.Event{
		ID:        "event-1",
		Timestamp: time.Now(),
		Actor:     event.Actor{ID: "actor-1", Type: event.ActorTypeService, IdentityConfidence: 1},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /x"},
		Target:    event.Target{Name: "svc-a"},
		Context:   event.Context{Environment: "prod", TraceID: "trace-1", SpanID: "span-1"},
	}
	varied := base
	varied.ID = "event-2"
	varied.Context.TraceID = "trace-2"
	varied.Context.SpanID = "span-2"

	fp1 := fingerprint.Compute(features.Extract(base).Stable)
	fp2 := fingerprint.Compute(features.Extract(varied).Stable)

	if fp1.ID != fp2.ID {
		t.Errorf("Fingerprint.ID changed when only Event.ID/TraceID/SpanID changed: %q vs %q", fp1.ID, fp2.ID)
	}
}
