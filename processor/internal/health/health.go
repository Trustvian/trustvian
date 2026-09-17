// Package health models the Trustvian runtime's liveness and readiness.
//
// It owns no transport. The state and the probe live here; HTTP lives in
// handler.go and is the only file that knows what a status code is. A
// future metric or CLI surface can read the same value without touching
// this type.
//
// # Liveness and readiness are different questions
//
// Liveness asks whether this process is functioning and able to keep
// executing. It deliberately does **not** consult the store: a supervisor
// that restarts a process because its database is unreachable produces a
// restart storm that cannot possibly help, because the database is not in
// the process. Liveness goes false only when the runtime begins shutting
// down — at which point it is not going to recover, and saying so lets a
// supervisor stop waiting.
//
// Readiness asks whether this instance can safely process its configured
// workload right now. It does consult the store. When PostgreSQL is
// configured and unusable, readiness is false — never a silent fall back to
// non-durable storage, which is the fail-closed persistence contract v0.8
// established.
package health

import (
	"context"
	"sync"
	"time"
)

// State is the runtime's lifecycle phase. It only ever moves forward:
// starting → running → draining. There is no path back, because a runtime
// that has begun shutting down does not resume.
type State int

const (
	// StateStarting is the initial phase: the runtime is alive but has not
	// finished initializing, so it must not be sent work yet.
	StateStarting State = iota

	// StateRunning means initialization completed. Readiness from here on
	// depends on whether the store is usable.
	StateRunning

	// StateDraining means shutdown has begun. Both liveness and readiness
	// are false.
	StateDraining
)

func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateDraining:
		return "draining"
	default:
		return "unknown"
	}
}

// ProbeFunc reports whether an external dependency is usable. It must be
// cheap and must honor the context's deadline — a readiness endpoint may be
// polled every few seconds, and it must never outlive its bound.
//
// A nil ProbeFunc means "no external dependency", which is the correct
// answer for the in-memory and file stores rather than a missing check:
// neither has anything that could be unavailable.
type ProbeFunc func(context.Context) error

// Health reports the runtime's liveness and readiness. Safe for concurrent
// use: probe endpoints are called from HTTP handlers while the lifecycle is
// advanced from the shutdown path.
type Health struct {
	// probeTimeout bounds every readiness probe. Set once at construction;
	// never mutated, so it needs no lock.
	probeTimeout time.Duration
	probe        ProbeFunc

	mu    sync.RWMutex
	state State
}

// New returns a Health in StateStarting — not ready until MarkRunning is
// called, so a runtime cannot accidentally report readiness before it has
// finished initializing.
//
// probe may be nil when the runtime has no external dependency to check.
func New(probe ProbeFunc, probeTimeout time.Duration) *Health {
	return &Health{
		probe:        probe,
		probeTimeout: probeTimeout,
		state:        StateStarting,
	}
}

// MarkRunning records that initialization finished. Ignored once draining:
// shutdown must not be undone by a late initialization callback.
func (h *Health) MarkRunning() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state == StateStarting {
		h.state = StateRunning
	}
}

// MarkDraining records that shutdown has begun. Idempotent, so a duplicate
// shutdown is harmless.
func (h *Health) MarkDraining() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = StateDraining
}

// State returns the current lifecycle phase.
func (h *Health) State() State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.state
}

// Live reports whether the runtime is alive.
//
// True in every phase except draining, and independent of the store: see
// the package comment for why a dependency outage must not fail liveness.
func (h *Health) Live() bool {
	return h.State() != StateDraining
}

// Ready reports whether the runtime can safely accept work, and the reason
// when it cannot.
//
// The returned error is for the runtime's own logs. It is deliberately not
// surfaced to the probe response, which carries a status and nothing else —
// an unauthenticated endpoint should not relay database errors.
func (h *Health) Ready(ctx context.Context) (bool, error) {
	switch h.State() {
	case StateStarting:
		return false, ErrStarting
	case StateDraining:
		return false, ErrDraining
	}

	if h.probe == nil {
		// No external dependency: ready by construction.
		return true, nil
	}

	// Bounded independently of whatever deadline the caller brought, so a
	// stuck dependency cannot hold the endpoint open.
	probeCtx, cancel := context.WithTimeout(ctx, h.probeTimeout)
	defer cancel()

	if err := h.probe(probeCtx); err != nil {
		return false, err
	}
	return true, nil
}
