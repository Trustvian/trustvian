package event_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
)

func validEvent() event.Event {
	return event.Event{
		ID:        "evt-1",
		Timestamp: time.Now(),
		Actor: event.Actor{
			ID:                 "svc-payment",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{
			Category:  event.OperationCategoryHTTP,
			Name:      "POST /payment",
			Direction: event.DirectionInbound,
		},
	}
}

func TestEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(event.Event) event.Event
		wantErr error
	}{
		{
			name:   "valid event",
			mutate: func(e event.Event) event.Event { return e },
		},
		{
			name: "optional fields left zero",
			mutate: func(e event.Event) event.Event {
				e.Target = event.Target{}
				e.Attributes = nil
				e.Context = event.Context{}
				return e
			},
		},
		{
			name:    "missing id",
			mutate:  func(e event.Event) event.Event { e.ID = ""; return e },
			wantErr: event.ErrMissingID,
		},
		{
			name:    "missing timestamp",
			mutate:  func(e event.Event) event.Event { e.Timestamp = time.Time{}; return e },
			wantErr: event.ErrMissingTimestamp,
		},
		{
			name:    "missing actor id",
			mutate:  func(e event.Event) event.Event { e.Actor.ID = ""; return e },
			wantErr: event.ErrMissingActorID,
		},
		{
			name:    "invalid actor type",
			mutate:  func(e event.Event) event.Event { e.Actor.Type = "root"; return e },
			wantErr: event.ErrInvalidActorType,
		},
		{
			name:    "identity confidence below range",
			mutate:  func(e event.Event) event.Event { e.Actor.IdentityConfidence = -0.1; return e },
			wantErr: event.ErrInvalidIdentityConfidence,
		},
		{
			name:    "identity confidence above range",
			mutate:  func(e event.Event) event.Event { e.Actor.IdentityConfidence = 1.1; return e },
			wantErr: event.ErrInvalidIdentityConfidence,
		},
		{
			name:    "invalid operation category",
			mutate:  func(e event.Event) event.Event { e.Operation.Category = "ftp"; return e },
			wantErr: event.ErrInvalidOperationCategory,
		},
		{
			name:    "missing operation name",
			mutate:  func(e event.Event) event.Event { e.Operation.Name = ""; return e },
			wantErr: event.ErrMissingOperationName,
		},
		{
			name:    "invalid operation direction",
			mutate:  func(e event.Event) event.Event { e.Operation.Direction = "sideways"; return e },
			wantErr: event.ErrInvalidOperationDirection,
		},
		{
			name: "ai agent tool call is a valid actor/operation combination",
			mutate: func(e event.Event) event.Event {
				e.Actor.Type = event.ActorTypeAIAgent
				e.Operation = event.Operation{
					Category: event.OperationCategoryTool,
					Name:     "search_customer",
				}
				return e
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mutate(validEvent()).Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want error wrapping %v", err, tt.wantErr)
			}
		})
	}
}

func TestEventValidateZeroValue(t *testing.T) {
	var e event.Event
	if err := e.Validate(); !errors.Is(err, event.ErrMissingID) {
		t.Fatalf("Validate() on zero-value Event = %v, want %v", err, event.ErrMissingID)
	}
}

func TestEventJSONRoundTrip(t *testing.T) {
	original := event.Event{
		ID:        "evt-1",
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Actor: event.Actor{
			ID:                 "svc-payment",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{
			Category:  event.OperationCategoryHTTP,
			Name:      "POST /payment",
			Direction: event.DirectionInbound,
		},
		Target:     event.Target{Name: "payment-db"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 12.5},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded event.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if !decoded.Timestamp.Equal(original.Timestamp) {
		t.Fatalf("Timestamp = %v, want %v", decoded.Timestamp, original.Timestamp)
	}
	decoded.Timestamp = original.Timestamp // avoid a spurious mismatch below from time.Time's internal representation

	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("round-tripped Event = %+v, want %+v", decoded, original)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatalf("round-tripped Event failed Validate(): %v", err)
	}
}

func TestTargetCategoryValid(t *testing.T) {
	tests := []struct {
		name string
		cat  event.TargetCategory
		want bool
	}{
		{"unspecified is valid", event.TargetCategoryUnspecified, true},
		{"internal", event.TargetCategoryInternal, true},
		{"external", event.TargetCategoryExternal, true},
		{"database", event.TargetCategoryDatabase, true},
		{"unknown value invalid", event.TargetCategory("bogus"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cat.Valid(); got != tt.want {
				t.Errorf("TargetCategory(%q).Valid() = %v, want %v", tt.cat, got, tt.want)
			}
		})
	}
}

func TestEventValidateIgnoresTargetCategory(t *testing.T) {
	e := validEvent()
	e.Target.Category = event.TargetCategory("not-a-real-category")
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil — TargetCategory must not be checked", err)
	}
}

func TestEventJSONOmitsUnsetOptionalFields(t *testing.T) {
	e := event.Event{
		ID:        "evt-1",
		Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		Actor:     event.Actor{ID: "svc-payment", Type: event.ActorTypeService},
		Operation: event.Operation{Category: event.OperationCategoryHTTP, Name: "GET /health"},
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	for _, field := range []string{`"target"`, `"attributes"`, `"context"`, `"direction"`} {
		if bytes.Contains(data, []byte(field)) {
			t.Errorf("Marshal() output contains unset optional field %s: %s", field, data)
		}
	}
}
