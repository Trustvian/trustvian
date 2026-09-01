// Package otel adapts a finished OpenTelemetry span into a Trustvian
// event.Event. It is the one place in this module that depends on OTel:
// the core engine (Event through Decision) has no knowledge of
// OpenTelemetry at all, and never will — see the package doc rationale
// in the repository's design notes. This adapter depends only on the
// OTel API and SDK's trace/attribute/resource packages, not the
// collector-builder toolchain; the OTel Collector processor is a
// separate, heavier deliverable and lives outside this module.
//
// EventFromSpan maps standard semantic-convention attributes where they
// exist (HTTP, DB, RPC, deployment.environment.name, service.name) and
// otherwise falls back to a small set of Trustvian-specific override
// attributes, documented here since none of this is standard OTel
// semconv:
//
//	trustvian.actor.id            overrides the inferred Actor.ID
//	trustvian.actor.type          overrides the inferred Actor.Type (e.g. "ai_agent")
//	trustvian.identity.confidence overrides the default Actor.IdentityConfidence
//	trustvian.operation.category  overrides the inferred Operation.Category (e.g. "tool")
//
// Every span attribute — mapped or not — is preserved in Event.Attributes,
// so nothing is silently dropped.
package otel

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
)

// Trustvian-specific override attributes. See the package doc for why
// these exist.
const (
	AttrActorID            = "trustvian.actor.id"
	AttrActorType          = "trustvian.actor.type"
	AttrIdentityConfidence = "trustvian.identity.confidence"
	AttrOperationCategory  = "trustvian.operation.category"
)

// EventFromSpan derives an event.Event from a finished span. It is a
// pure function of span: identical input always produces identical
// output, and span is never modified.
//
// The result is a best-effort mapping, not a validated Event — callers
// should still call Validate (or rely on Engine.Analyze doing so) before
// treating it as a valid pipeline input. A span whose attributes and
// resource carry no usable actor identity, for instance, will produce an
// Event with an empty Actor.ID, which Validate correctly rejects rather
// than the adapter inventing one.
func EventFromSpan(span sdktrace.ReadOnlySpan) event.Event {
	attrs := attributeMap(span.Attributes())
	bridgeVolatileSignals(attrs, span)

	actorID := stringAttr(attrs, AttrActorID)
	if actorID == "" {
		actorID = resourceAttr(span, semconv.ServiceNameKey)
	}

	actorType := event.ActorType(stringAttr(attrs, AttrActorType))
	if actorType == "" {
		actorType = event.ActorTypeService
	}

	identityConfidence := 1.0
	if v, ok := floatAttr(attrs, AttrIdentityConfidence); ok {
		identityConfidence = v
	}

	category := event.OperationCategory(stringAttr(attrs, AttrOperationCategory))
	if category == "" {
		category = inferCategory(attrs)
	}

	sc := span.SpanContext()

	return event.Event{
		ID:        sc.SpanID().String(),
		Timestamp: span.StartTime(),
		Actor: event.Actor{
			ID:                 actorID,
			Type:               actorType,
			IdentityConfidence: identityConfidence,
		},
		Operation: event.Operation{
			Category:  category,
			Name:      span.Name(),
			Direction: directionFromSpanKind(span.SpanKind()),
		},
		Target:     event.Target{Name: targetName(attrs)},
		Attributes: attrs,
		Context: event.Context{
			Environment: resourceAttr(span, semconv.DeploymentEnvironmentNameKey),
			TraceID:     sc.TraceID().String(),
			SpanID:      sc.SpanID().String(),
		},
	}
}

// bridgeVolatileSignals sets the features.AttrDurationMS/AttrError keys
// from span timing and status, since these are span-level fields, not
// span attributes, and features.Extract only ever looks at
// Event.Attributes.
func bridgeVolatileSignals(attrs map[string]any, span sdktrace.ReadOnlySpan) {
	if end := span.EndTime(); !end.IsZero() {
		if dur := end.Sub(span.StartTime()); dur > 0 {
			attrs[features.AttrDurationMS] = float64(dur) / float64(time.Millisecond)
		}
	}
	if span.Status().Code == codes.Error {
		attrs[features.AttrError] = true
	}
}

func attributeMap(kvs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		m[string(kv.Key)] = attributeValue(kv.Value)
	}
	return m
}

func attributeValue(v attribute.Value) any {
	switch v.Type() {
	case attribute.BOOL:
		return v.AsBool()
	case attribute.INT64:
		return v.AsInt64()
	case attribute.FLOAT64:
		return v.AsFloat64()
	case attribute.STRING:
		return v.AsString()
	case attribute.BOOLSLICE:
		return v.AsBoolSlice()
	case attribute.INT64SLICE:
		return v.AsInt64Slice()
	case attribute.FLOAT64SLICE:
		return v.AsFloat64Slice()
	case attribute.STRINGSLICE:
		return v.AsStringSlice()
	default:
		return v.String()
	}
}

func stringAttr(attrs map[string]any, key string) string {
	v, _ := attrs[key].(string)
	return v
}

func floatAttr(attrs map[string]any, key string) (float64, bool) {
	switch v := attrs[key].(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

// inferCategory classifies the span by which family of semantic
// convention attributes it carries, checked in this order: HTTP, DB,
// RPC. A span matching none of them falls back to "rpc" — the most
// generic "some kind of call happened" bucket among Trustvian's five
// operation categories.
func inferCategory(attrs map[string]any) event.OperationCategory {
	if _, ok := attrs[string(semconv.HTTPRequestMethodKey)]; ok {
		return event.OperationCategoryHTTP
	}
	if _, ok := attrs[string(semconv.DBSystemNameKey)]; ok {
		return event.OperationCategoryDB
	}
	return event.OperationCategoryRPC
}

// targetName picks the destination attribute with the most specific
// available meaning: the logical peer service name, then the database
// namespace, then the raw network address.
func targetName(attrs map[string]any) string {
	if v := stringAttr(attrs, string(semconv.ServicePeerNameKey)); v != "" {
		return v
	}
	if v := stringAttr(attrs, string(semconv.DBNamespaceKey)); v != "" {
		return v
	}
	return stringAttr(attrs, string(semconv.ServerAddressKey))
}

func directionFromSpanKind(kind trace.SpanKind) event.OperationDirection {
	switch kind {
	case trace.SpanKindServer, trace.SpanKindConsumer:
		return event.DirectionInbound
	case trace.SpanKindClient, trace.SpanKindProducer:
		return event.DirectionOutbound
	default:
		return event.DirectionUnspecified
	}
}

func resourceAttr(span sdktrace.ReadOnlySpan, key attribute.Key) string {
	res := span.Resource()
	if res == nil {
		return ""
	}
	for _, kv := range res.Attributes() {
		if kv.Key == key {
			return kv.Value.AsString()
		}
	}
	return ""
}
