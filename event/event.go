// Package event defines Trustvian's Event domain model: the single,
// generic shape that every stage of the pipeline (features, fingerprint,
// baseline, anomaly, trust, policy) consumes as input.
//
// Event and its nested types are treated as immutable value types: callers
// construct a new value rather than mutating fields of an existing one, and
// nothing in this package hands back a pointer that aliases internal state.
//
// JSON struct tags are provided so an Event can be read from a file (see
// cmd/trustvian) or any other JSON source; this is a serialization
// convenience only and does not make JSON Trustvian's canonical wire
// format.
package event

import (
	"errors"
	"fmt"
	"math"
	"time"
)

// Sentinel errors returned by Validate, usable with errors.Is.
var (
	ErrMissingID                 = errors.New("event: missing id")
	ErrMissingTimestamp          = errors.New("event: missing timestamp")
	ErrMissingActorID            = errors.New("event: missing actor id")
	ErrInvalidActorType          = errors.New("event: invalid actor type")
	ErrInvalidIdentityConfidence = errors.New("event: identity confidence out of range [0,1]")
	ErrMissingOperationName      = errors.New("event: missing operation name")
	ErrInvalidOperationCategory  = errors.New("event: invalid operation category")
	ErrInvalidOperationDirection = errors.New("event: invalid operation direction")
)

// ActorType identifies the kind of thing that performed an Operation.
type ActorType string

const (
	ActorTypeService        ActorType = "service"
	ActorTypeUser           ActorType = "user"
	ActorTypeServiceAccount ActorType = "service_account"
	ActorTypeAIAgent        ActorType = "ai_agent"
	ActorTypeDevice         ActorType = "device"
	ActorTypeUnknown        ActorType = "unknown"
)

func (t ActorType) valid() bool {
	switch t {
	case ActorTypeService, ActorTypeUser, ActorTypeServiceAccount, ActorTypeAIAgent, ActorTypeDevice, ActorTypeUnknown:
		return true
	default:
		return false
	}
}

// Actor is who or what performed the Operation.
//
// IdentityConfidence reflects how much upstream authentication/telemetry
// vouches for Actor.ID; it is an input Trustvian trusts, not something
// Trustvian itself computes.
type Actor struct {
	ID                 string    `json:"id"`
	Type               ActorType `json:"type"`
	IdentityConfidence float64   `json:"identity_confidence"`
}

func (a Actor) validate() error {
	if a.ID == "" {
		return ErrMissingActorID
	}
	if !a.Type.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidActorType, a.Type)
	}
	if math.IsNaN(a.IdentityConfidence) || a.IdentityConfidence < 0 || a.IdentityConfidence > 1 {
		return fmt.Errorf("%w: %v", ErrInvalidIdentityConfidence, a.IdentityConfidence)
	}
	return nil
}

// OperationCategory classifies what kind of action an Operation represents.
type OperationCategory string

const (
	OperationCategoryHTTP     OperationCategory = "http"
	OperationCategoryDB       OperationCategory = "db"
	OperationCategoryRPC      OperationCategory = "rpc"
	OperationCategoryTool     OperationCategory = "tool"
	OperationCategoryExternal OperationCategory = "external"
)

func (c OperationCategory) valid() bool {
	switch c {
	case OperationCategoryHTTP, OperationCategoryDB, OperationCategoryRPC, OperationCategoryTool, OperationCategoryExternal:
		return true
	default:
		return false
	}
}

// OperationDirection indicates whether the Operation was initiated by the
// Actor (outbound) or received by it (inbound). It is optional; the zero
// value means unspecified.
type OperationDirection string

const (
	DirectionUnspecified OperationDirection = ""
	DirectionInbound     OperationDirection = "inbound"
	DirectionOutbound    OperationDirection = "outbound"
)

func (d OperationDirection) valid() bool {
	switch d {
	case DirectionUnspecified, DirectionInbound, DirectionOutbound:
		return true
	default:
		return false
	}
}

// TargetCategory classifies what kind of destination a Target is. It is
// optional; the zero value means "unclassified" — a producer may set it or
// leave it unset, matching how Direction is already optional today.
type TargetCategory string

const (
	TargetCategoryUnspecified TargetCategory = ""
	TargetCategoryInternal    TargetCategory = "internal"
	TargetCategoryExternal    TargetCategory = "external"
	TargetCategoryDatabase    TargetCategory = "database"
)

func (c TargetCategory) valid() bool {
	switch c {
	case TargetCategoryUnspecified, TargetCategoryInternal, TargetCategoryExternal, TargetCategoryDatabase:
		return true
	default:
		return false
	}
}

// Operation is what the Actor did: e.g. an HTTP route, a DB query, an RPC
// call, an AI-agent tool invocation, or a call to an external destination.
type Operation struct {
	Category  OperationCategory  `json:"category"`
	Name      string             `json:"name"`
	Direction OperationDirection `json:"direction,omitempty"`
}

func (o Operation) validate() error {
	if !o.Category.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidOperationCategory, o.Category)
	}
	if o.Name == "" {
		return ErrMissingOperationName
	}
	if !o.Direction.valid() {
		return fmt.Errorf("%w: %q", ErrInvalidOperationDirection, o.Direction)
	}
	return nil
}

// Target is the destination of the Operation: a service name, a database,
// or an external host. It is optional — not every Operation has a distinct
// destination. Category, when set, classifies what kind of destination
// this is (see TargetCategory); it is not required for Validate to pass.
type Target struct {
	Name     string         `json:"name"`
	Category TargetCategory `json:"category,omitzero"`
}

// Context carries correlation and scoping data for an Event: the
// deployment environment and, when available, OpenTelemetry trace/span
// identifiers.
type Context struct {
	Environment string `json:"environment,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
}

// Event is an immutable, atomic observed action: the input to every stage
// of the Trustvian pipeline.
type Event struct {
	ID         string         `json:"id"`
	Timestamp  time.Time      `json:"timestamp"`
	Actor      Actor          `json:"actor"`
	Operation  Operation      `json:"operation"`
	Target     Target         `json:"target,omitzero"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Context    Context        `json:"context,omitzero"`
}

// Validate reports whether e has all fields required for pipeline
// processing. Target, Attributes, and Context are optional and are not
// checked.
func (e Event) Validate() error {
	if e.ID == "" {
		return ErrMissingID
	}
	if e.Timestamp.IsZero() {
		return ErrMissingTimestamp
	}
	if err := e.Actor.validate(); err != nil {
		return err
	}
	if err := e.Operation.validate(); err != nil {
		return err
	}
	return nil
}
