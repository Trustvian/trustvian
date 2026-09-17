// Package metrics instruments what Trustvian uniquely knows about its own
// operation: what its engine decided, how long its analysis took, and
// whether its learning path succeeded.
//
// # What is deliberately not here
//
// The Collector already counts spans accepted, refused, and dropped per
// component, and already reports process memory, CPU, and uptime. Emitting
// a Trustvian span counter would produce a second, subtly different number
// for the same thing and force operators to learn which to trust. Nothing
// in this package duplicates a Collector-native signal.
//
// # Cardinality is a hard bound, not a guideline
//
// Every attribute here has a closed vocabulary enumerated as constants.
// Nothing is derived from input. The whole package produces a fixed 15 time
// series no matter how many actors, events, or environments a deployment
// sees — which is what makes it safe to leave on permanently.
//
// 15, not 14: the decision vocabulary is the six policy.Decision values
// plus DecisionOther, which exists precisely so an unrecognized value
// cannot open a new series.
//
// Actor IDs, session IDs, trace and span IDs, target and operation names,
// fingerprints, environments, and raw error strings are all forbidden as
// attributes. Each is either unbounded or is data about a *subject* rather
// than about Trustvian. The behavioral detail already travels on the span,
// where it belongs and where sampling applies.
//
// # These metrics never re-enter the engine
//
// Operational telemetry describes Trustvian; behavioral telemetry
// describes its subjects. Routing the first into the second would make the
// engine analyze its own analysis, producing anomaly signals about nothing.
// This package only records; it has no path back into an Engine.
package metrics

import (
	"context"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ScopeName identifies this instrumentation in emitted telemetry, matching
// the module that produces it.
const ScopeName = "trustvian-processor"

// Attribute keys. Namespaced so they cannot collide with the Collector's
// own or with semantic-convention attributes.
const (
	attrOutcome  = attribute.Key("trustvian.outcome")
	attrDecision = attribute.Key("trustvian.decision")
)

// Analysis outcomes — every way a span can end in this processor. Three,
// exhaustively.
const (
	OutcomeAnalyzed     = "analyzed"
	OutcomeInvalidEvent = "invalid_event"
	OutcomeError        = "error"
)

// Observation outcomes — the learning-eligibility gate's own results.
const (
	OutcomeLearned     = "learned"
	OutcomeNotEligible = "not_eligible"
)

// knownDecisions is the closed vocabulary of policy decisions.
//
// Enumerated rather than accepting whatever string arrives: a decision
// value is domain data today, but a future one added upstream would
// otherwise expand this metric's cardinality silently, before anyone
// noticed. Anything outside the set records as DecisionOther, which keeps
// the bound fixed and makes the omission visible in the data.
var knownDecisions = []string{
	"allow",
	"observe_only",
	"alert",
	"challenge",
	"require_approval",
	"block",
}

// analysisOutcomes and observeOutcomes are the two outcome vocabularies,
// each exhaustive for its instrument.
var (
	analysisOutcomes = []string{OutcomeAnalyzed, OutcomeInvalidEvent, OutcomeError}
	observeOutcomes  = []string{OutcomeLearned, OutcomeNotEligible, OutcomeError}
)

// DecisionOther is recorded for any decision value not in knownDecisions.
const DecisionOther = "other"

// durationBucketsSeconds is advice for both latency histograms: the OTel
// semantic conventions' recommended boundaries for a duration measured in
// seconds.
//
// Stated explicitly because the SDK's default boundaries — 0, 5, 10, 25,
// … 10000 — assume milliseconds. Recording seconds against them puts every
// realistic measurement in the first bucket, which was exactly what a live
// Collector scrape showed before this was added: a histogram that counts
// correctly and describes nothing. These boundaries span 1 ms to 10 s,
// which covers an in-memory analysis (microseconds) through a
// pathologically slow database round trip.
var durationBucketsSeconds = []float64{
	0.001, 0.0025, 0.005, 0.0075,
	0.01, 0.025, 0.05, 0.075,
	0.1, 0.25, 0.5, 0.75,
	1, 2.5, 5, 7.5, 10,
}

// Metrics holds this processor's instruments.
//
// A value, constructed from an injected metric.Meter — no package-level
// state, so tests construct their own with an in-memory reader and nothing
// is shared between processor instances.
//
// The zero value is usable and records nothing, so a caller never has to
// nil-check before recording.
type Metrics struct {
	analyses        metric.Int64Counter
	decisions       metric.Int64Counter
	analysisLatency metric.Float64Histogram
	observations    metric.Int64Counter
	observeLatency  metric.Float64Histogram

	// One pre-built measurement option per vocabulary entry, so recording
	// is a map lookup instead of building an attribute.Set per span.
	//
	// This is the closed vocabulary paying for itself twice. It removes
	// the per-call set construction (sort, dedup, allocate) that
	// metric.WithAttributes does on every Add — measured at ~345 ns per
	// span across the three record calls, on a path whose whole analysis
	// costs ~1.4 µs.
	//
	// It also makes the cardinality bound structural rather than checked:
	// a value with no entry in these maps has no option to record with, so
	// an unbounded label cannot reach an instrument even by mistake.
	analysisOpts map[string][]metric.AddOption
	decisionOpts map[string][]metric.AddOption
	observeOpts  map[string][]metric.AddOption
}

// attributeOptions pre-builds one measurement option per value in a
// closed vocabulary.
//
// WithAttributeSet, not WithAttributes: the set is constructed once here,
// so the recording path does no sorting or allocation at all.
//
// The option is stored already wrapped in its variadic slice. Add takes
// ...AddOption, and a slice built at the call site escapes into the SDK —
// one allocation per recorded measurement, three per span. Passing a
// pre-built slice removes them, which is why this is []AddOption rather
// than the bare MeasurementOption it wraps.
func attributeOptions(key attribute.Key, values []string) map[string][]metric.AddOption {
	opts := make(map[string][]metric.AddOption, len(values))
	for _, v := range values {
		opts[v] = []metric.AddOption{metric.WithAttributeSet(attribute.NewSet(key.String(v)))}
	}
	return opts
}

// New builds the instruments from meter.
//
// An error here means an instrument name was rejected by the SDK, which is
// a programming error rather than a runtime condition; the caller decides
// whether to fail startup or continue uninstrumented. Nothing in this
// package makes recording fallible.
func New(meter metric.Meter) (*Metrics, error) {
	var (
		m   Metrics
		err error
	)

	// Units follow UCUM. Counters carry no "total"/"count" suffix —
	// exporters add those per their own conventions.
	if m.analyses, err = meter.Int64Counter(
		"trustvian.analyses",
		metric.WithUnit("{analysis}"),
		metric.WithDescription("Spans Trustvian attempted to analyze, by outcome."),
	); err != nil {
		return nil, err
	}

	if m.decisions, err = meter.Int64Counter(
		"trustvian.decisions",
		metric.WithUnit("{decision}"),
		metric.WithDescription("Policy decisions produced, by decision."),
	); err != nil {
		return nil, err
	}

	// Seconds, the OTel convention for new duration instruments. No
	// attributes: splitting latency by outcome would tell an operator
	// little that the counters do not, and every attribute multiplies the
	// histogram's bucket count.
	if m.analysisLatency, err = meter.Float64Histogram(
		"trustvian.analysis.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Trustvian's own analysis of one event."),
		metric.WithExplicitBucketBoundaries(durationBucketsSeconds...),
	); err != nil {
		return nil, err
	}

	if m.observations, err = meter.Int64Counter(
		"trustvian.observations",
		metric.WithUnit("{observation}"),
		metric.WithDescription("Learning attempts, by outcome."),
	); err != nil {
		return nil, err
	}

	if m.observeLatency, err = meter.Float64Histogram(
		"trustvian.observe.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of folding one observation into the baseline, including storage."),
		metric.WithExplicitBucketBoundaries(durationBucketsSeconds...),
	); err != nil {
		return nil, err
	}

	m.analysisOpts = attributeOptions(attrOutcome, analysisOutcomes)
	m.observeOpts = attributeOptions(attrOutcome, observeOutcomes)
	// Concat, not append: appending to a package-level slice would
	// share its backing array with every other caller.
	m.decisionOpts = attributeOptions(attrDecision, slices.Concat(knownDecisions, []string{DecisionOther}))

	return &m, nil
}

// RecordAnalysis records one analysis attempt and, when it produced a
// result, its duration.
//
// Recording is an in-process update; export is the SDK's concern and never
// blocks this call. A failing telemetry backend therefore cannot slow or
// alter a decision.
func (m *Metrics) RecordAnalysis(ctx context.Context, outcome string, d time.Duration) {
	if m == nil || m.analyses == nil {
		return
	}
	opt, ok := m.analysisOpts[outcome]
	if !ok {
		// Unreachable from this module — every caller passes a constant —
		// but recording nothing beats recording an unbounded label.
		return
	}
	m.analyses.Add(ctx, 1, opt...)
	if outcome == OutcomeAnalyzed {
		m.analysisLatency.Record(ctx, d.Seconds())
	}
}

// RecordDecision records one policy decision, mapping any unrecognized
// value to DecisionOther so cardinality stays fixed.
func (m *Metrics) RecordDecision(ctx context.Context, decision string) {
	if m == nil || m.decisions == nil {
		return
	}
	opt, ok := m.decisionOpts[decision]
	if !ok {
		opt = m.decisionOpts[DecisionOther]
	}
	m.decisions.Add(ctx, 1, opt...)
}

// RecordObservation records one learning attempt and its duration.
//
// Duration is recorded for every outcome, unlike analysis: an Observe that
// fails has still paid the storage round trip, and that latency is exactly
// what an operator investigating a slow database wants to see.
func (m *Metrics) RecordObservation(ctx context.Context, outcome string, d time.Duration) {
	if m == nil || m.observations == nil {
		return
	}
	opt, ok := m.observeOpts[outcome]
	if !ok {
		return
	}
	m.observations.Add(ctx, 1, opt...)
	m.observeLatency.Record(ctx, d.Seconds())
}
