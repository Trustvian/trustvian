package trustvianprocessor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
)

// Stats is a snapshot of this processor's observable counters: how
// many spans it has processed, how many didn't map to a valid Event or
// failed Analyze, and how many landed on each Decision — the
// concrete "observable" requirement task 009 names.
type Stats struct {
	SpansProcessed uint64
	Invalid        uint64
	AnalyzeErrors  uint64
	Decisions      map[string]uint64
}

// trustvianProcessor scores each span in a trace batch through a
// Trustvian Engine and enriches it with the resulting trustvian.*
// attributes, then forwards the (now-enriched) batch unchanged in
// shape to the next consumer in the pipeline.
type trustvianProcessor struct {
	engine *trustvian.Engine
	next   consumer.Traces
	logger *zap.Logger

	processed     atomic.Uint64
	invalid       atomic.Uint64
	analyzeErrors atomic.Uint64

	mu        sync.Mutex
	decisions map[string]uint64
}

// newTrustvianProcessor constructs the processor's Engine from cfg.
//
// cfg.Policy == nil (the field's zero value, meaning the Collector
// config had no `policy:` block at all) preserves this processor's
// original behavior exactly: trustvian.NewEngine() with no options,
// i.e. its own default Policy (no rules, ObserveOnly fallback).
//
// cfg.Policy != nil — including an explicitly empty `policy: {}` block
// — is treated as an explicit request for a configured Policy: it is
// decoded, validated, and compiled via decodePolicy/config.CompilePolicy,
// the exact same path the Go SDK and CLI use. Any failure in that path
// (a decode error, an invalid schema version, a missing default, an
// invalid decision/condition value) is returned here rather than
// falling back to the default Policy — this is what lets
// createTracesProcessor fail Collector startup outright on a bad
// explicit config instead of silently weakening it.
func newTrustvianProcessor(set component.TelemetrySettings, next consumer.Traces, cfg *Config) (*trustvianProcessor, error) {
	var opts []trustvian.Option
	if cfg.Policy != nil {
		pc, err := decodePolicy(cfg.Policy)
		if err != nil {
			return nil, err
		}
		p, err := config.CompilePolicy(pc)
		if err != nil {
			return nil, fmt.Errorf("trustvianprocessor: policy: %w", err)
		}
		opts = append(opts, trustvian.WithPolicy(p))
	}

	return &trustvianProcessor{
		engine:    trustvian.NewEngine(opts...),
		next:      next,
		logger:    set.Logger,
		decisions: make(map[string]uint64),
	}, nil
}

// Capabilities reports that this processor mutates its input in place
// (it writes trustvian.* attributes directly onto each span) — the
// Collector's own contract for processors that don't copy their input
// before modifying it.
func (p *trustvianProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

// Start and Shutdown are no-ops: this processor holds no external
// connections, background goroutines, or resources beyond the Engine
// itself (which is fully synchronous — see docs/PERFORMANCE.md
// § Concurrency considerations in the core repository).
func (p *trustvianProcessor) Start(_ context.Context, _ component.Host) error { return nil }
func (p *trustvianProcessor) Shutdown(_ context.Context) error                { return nil }

// ConsumeTraces scores every span in td and forwards td (now enriched
// in place) to the next consumer. A span that doesn't map to a
// Validate-passing Event, or whose Analyze call errors, is left
// un-enriched and counted, but never stops the batch: one malformed
// span must not drop every other span in the same trace.
func (p *trustvianProcessor) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	for _, rs := range td.ResourceSpans().All() {
		resourceAttrs := rs.Resource().Attributes()
		for _, ss := range rs.ScopeSpans().All() {
			spans := ss.Spans()
			for i := range spans.Len() {
				p.processSpan(ctx, resourceAttrs, spans.At(i))
			}
		}
	}
	return p.next.ConsumeTraces(ctx, td)
}

func (p *trustvianProcessor) processSpan(ctx context.Context, resourceAttrs pcommon.Map, span ptrace.Span) {
	p.processed.Add(1)

	ev := EventFromSpan(resourceAttrs, span)
	if err := ev.Validate(); err != nil {
		p.invalid.Add(1)
		p.logger.Debug("trustvianprocessor: span did not map to a valid Event",
			zap.String("span", span.Name()), zap.Error(err))
		return
	}

	result, err := p.engine.Analyze(ctx, ev)
	if err != nil {
		p.analyzeErrors.Add(1)
		p.logger.Warn("trustvianprocessor: Analyze failed",
			zap.String("span", span.Name()), zap.Error(err))
		return
	}

	SetAttributesFromResult(span.Attributes(), result)
	p.recordDecision(string(result.Decision))

	// Observe is always safe to call unconditionally — it is a no-op
	// for any Decision that isn't learning-eligible (see the core
	// repository's docs/SECURITY.md § baseline poisoning).
	if _, err := p.engine.Observe(ctx, result); err != nil {
		p.logger.Warn("trustvianprocessor: Observe failed",
			zap.String("span", span.Name()), zap.Error(err))
	}
}

func (p *trustvianProcessor) recordDecision(decision string) {
	p.mu.Lock()
	p.decisions[decision]++
	p.mu.Unlock()
}

// Stats returns a snapshot of this processor's observable counters.
func (p *trustvianProcessor) Stats() Stats {
	p.mu.Lock()
	decisions := make(map[string]uint64, len(p.decisions))
	for k, v := range p.decisions {
		decisions[k] = v
	}
	p.mu.Unlock()

	return Stats{
		SpansProcessed: p.processed.Load(),
		Invalid:        p.invalid.Load(),
		AnalyzeErrors:  p.analyzeErrors.Load(),
		Decisions:      decisions,
	}
}

var (
	_ consumer.Traces     = (*trustvianProcessor)(nil)
	_ component.Component = (*trustvianProcessor)(nil)
)
