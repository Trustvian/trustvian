package features_test

import (
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
)

func baseEvent(category event.OperationCategory, name string, attrs map[string]any) event.Event {
	return event.Event{
		ID:        "evt-1",
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Actor: event.Actor{
			ID:                 "actor-1",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.9,
		},
		Operation: event.Operation{
			Category: category,
			Name:     name,
		},
		Target:     event.Target{Name: "payment-db"},
		Context:    event.Context{Environment: "production"},
		Attributes: attrs,
	}
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name       string
		event      event.Event
		wantStable features.StableFeatures
		wantVol    features.VolatileFeatures
	}{
		{
			name: "http event with float64 duration and error",
			event: baseEvent(event.OperationCategoryHTTP, "POST /payment", map[string]any{
				features.AttrDurationMS: float64(120.5),
				features.AttrError:      true,
			}),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryHTTP,
				OperationName:     "POST /payment",
				TargetName:        "payment-db",
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				Latency:    120*time.Millisecond + 500*time.Microsecond,
				HasLatency: true,
				Error:      true,
			},
		},
		{
			name: "db event with int duration, no error",
			event: baseEvent(event.OperationCategoryDB, "SELECT accounts", map[string]any{
				features.AttrDurationMS: int(42),
			}),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryDB,
				OperationName:     "SELECT accounts",
				TargetName:        "payment-db",
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				Latency:    42 * time.Millisecond,
				HasLatency: true,
				Error:      false,
			},
		},
		{
			name: "rpc event with int64 duration",
			event: baseEvent(event.OperationCategoryRPC, "OrderService.Create", map[string]any{
				features.AttrDurationMS: int64(7),
			}),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryRPC,
				OperationName:     "OrderService.Create",
				TargetName:        "payment-db",
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				Latency:    7 * time.Millisecond,
				HasLatency: true,
			},
		},
		{
			name:  "tool event with no attributes at all",
			event: baseEvent(event.OperationCategoryTool, "search_customer", nil),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryTool,
				OperationName:     "search_customer",
				TargetName:        "payment-db",
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				HasLatency: false,
				Error:      false,
			},
		},
		{
			name: "target category set flows through",
			event: func() event.Event {
				e := baseEvent(event.OperationCategoryDB, "SELECT accounts", nil)
				e.Target.Category = event.TargetCategoryDatabase
				return e
			}(),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryDB,
				OperationName:     "SELECT accounts",
				TargetName:        "payment-db",
				TargetCategory:    event.TargetCategoryDatabase,
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				HasLatency: false,
				Error:      false,
			},
		},
		{
			name:  "target category unset yields zero value",
			event: baseEvent(event.OperationCategoryHTTP, "GET /health", nil),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryHTTP,
				OperationName:     "GET /health",
				TargetName:        "payment-db",
				TargetCategory:    event.TargetCategoryUnspecified,
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				HasLatency: false,
				Error:      false,
			},
		},
		{
			name: "external event with wrong-typed attributes ignored",
			event: baseEvent(event.OperationCategoryExternal, "webhook.send", map[string]any{
				features.AttrDurationMS: "fast",
				features.AttrError:      "yes",
			}),
			wantStable: features.StableFeatures{
				ActorType:         event.ActorTypeService,
				OperationCategory: event.OperationCategoryExternal,
				OperationName:     "webhook.send",
				TargetName:        "payment-db",
				Environment:       "production",
			},
			wantVol: features.VolatileFeatures{
				Timestamp:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
				HasLatency: false,
				Error:      false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := features.Extract(tt.event)
			if got.Stable != tt.wantStable {
				t.Errorf("Stable = %+v, want %+v", got.Stable, tt.wantStable)
			}
			if got.Volatile != tt.wantVol {
				t.Errorf("Volatile = %+v, want %+v", got.Volatile, tt.wantVol)
			}
		})
	}
}

func TestExtractDeterministic(t *testing.T) {
	e := baseEvent(event.OperationCategoryHTTP, "GET /health", map[string]any{
		features.AttrDurationMS: float64(3),
	})

	first := features.Extract(e)
	second := features.Extract(e)

	if first != second {
		t.Fatalf("Extract is not deterministic: first=%+v second=%+v", first, second)
	}
}

func TestExtractDoesNotMutateEvent(t *testing.T) {
	attrs := map[string]any{features.AttrDurationMS: float64(10)}
	e := baseEvent(event.OperationCategoryHTTP, "GET /health", attrs)

	_ = features.Extract(e)

	if len(attrs) != 1 {
		t.Fatalf("Extract mutated the source attributes map: %v", attrs)
	}
}
