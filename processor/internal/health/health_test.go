package health

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

var errProbe = errors.New("dependency unavailable")

func failingProbe(context.Context) error { return errProbe }
func okProbe(context.Context) error      { return nil }

// hangingProbe never returns on its own — it returns only when its context
// is done. This is how the timeout test stays deterministic: the probe's
// completion is caused by the bound under test, not by a sleep racing it.
func hangingProbe(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestStartingIsLiveButNotReady(t *testing.T) {
	h := New(okProbe, time.Second)

	if !h.Live() {
		t.Error("Live() = false while starting, want true — a starting runtime is alive")
	}
	ready, err := h.Ready(context.Background())
	if ready {
		t.Error("Ready() = true before MarkRunning, want false")
	}
	if !errors.Is(err, ErrStarting) {
		t.Errorf("Ready() err = %v, want ErrStarting", err)
	}
}

func TestRunningWithHealthyProbeIsReady(t *testing.T) {
	h := New(okProbe, time.Second)
	h.MarkRunning()

	if !h.Live() {
		t.Error("Live() = false while running, want true")
	}
	if ready, err := h.Ready(context.Background()); !ready {
		t.Errorf("Ready() = false, %v; want true", err)
	}
}

// TestFailingProbeDoesNotAffectLiveness is the anti-restart-storm property:
// a database outage must never make a supervisor kill a healthy process,
// because restarting cannot fix a dependency that lives elsewhere.
func TestFailingProbeDoesNotAffectLiveness(t *testing.T) {
	h := New(failingProbe, time.Second)
	h.MarkRunning()

	if !h.Live() {
		t.Error("Live() = false when the dependency probe fails, want true — liveness must not depend on the store")
	}
	ready, err := h.Ready(context.Background())
	if ready {
		t.Error("Ready() = true with a failing probe, want false")
	}
	if !errors.Is(err, errProbe) {
		t.Errorf("Ready() err = %v, want the probe's error", err)
	}
}

// TestNilProbeIsReady pins that "no external dependency" is a correct
// answer rather than a skipped check: InMemory and FileStore have nothing
// that can become unavailable.
func TestNilProbeIsReady(t *testing.T) {
	h := New(nil, time.Second)
	h.MarkRunning()

	if ready, err := h.Ready(context.Background()); !ready {
		t.Errorf("Ready() = false, %v; want true for a store with no external dependency", err)
	}
}

func TestDrainingIsNeitherLiveNorReady(t *testing.T) {
	h := New(okProbe, time.Second)
	h.MarkRunning()
	h.MarkDraining()

	if h.Live() {
		t.Error("Live() = true while draining, want false")
	}
	ready, err := h.Ready(context.Background())
	if ready {
		t.Error("Ready() = true while draining, want false")
	}
	if !errors.Is(err, ErrDraining) {
		t.Errorf("Ready() err = %v, want ErrDraining", err)
	}
}

// TestDrainingIsNotUndoneByLateMarkRunning guards a real shape: Start and
// Shutdown can race during a failed startup, and a late initialization
// callback must not resurrect a runtime that has begun shutting down.
func TestDrainingIsNotUndoneByLateMarkRunning(t *testing.T) {
	h := New(okProbe, time.Second)
	h.MarkDraining()
	h.MarkRunning()

	if h.Live() {
		t.Error("MarkRunning() after MarkDraining() made the runtime live again")
	}
	if h.State() != StateDraining {
		t.Errorf("State() = %v after a late MarkRunning, want draining", h.State())
	}
}

func TestMarkDrainingIsIdempotent(t *testing.T) {
	h := New(okProbe, time.Second)
	h.MarkRunning()
	for range 3 {
		h.MarkDraining() // must not panic
	}
	if h.Live() {
		t.Error("Live() = true after repeated MarkDraining")
	}
}

// TestReadyIsBoundedByProbeTimeout is §13's requirement: a wedged
// dependency must not hold the readiness endpoint open. Deterministic
// because the probe returns exactly when its context expires.
func TestReadyIsBoundedByProbeTimeout(t *testing.T) {
	const timeout = 50 * time.Millisecond
	h := New(hangingProbe, timeout)
	h.MarkRunning()

	start := time.Now()
	ready, err := h.Ready(context.Background())
	elapsed := time.Since(start)

	if ready {
		t.Error("Ready() = true for a probe that never succeeds, want false")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Ready() err = %v, want a deadline error", err)
	}
	// Generous upper bound: the assertion is "bounded", not "precisely timed".
	if elapsed > timeout*10 {
		t.Errorf("Ready() took %v with a %v timeout — the probe is not bounded", elapsed, timeout)
	}
}

// TestReadyHonorsCallerCancellation proves the probe's own bound does not
// override a caller that gave up sooner — an abandoned HTTP request should
// not keep a probe running.
func TestReadyHonorsCallerCancellation(t *testing.T) {
	h := New(hangingProbe, time.Hour) // far longer than the caller will wait
	h.MarkRunning()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if ready, _ := h.Ready(ctx); ready {
			t.Error("Ready() = true for a cancelled caller context")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Ready() ignored the caller's cancellation")
	}
}

// TestConcurrentAccessIsRaceFree exercises the pattern that actually
// occurs: probe endpoints reading while the shutdown path advances state.
// Meaningful under -race, which CI runs.
func TestConcurrentAccessIsRaceFree(t *testing.T) {
	h := New(okProbe, time.Second)
	h.MarkRunning()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				h.Live()
				_, _ = h.Ready(context.Background())
				h.State()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.MarkDraining()
	}()
	wg.Wait()
}

func TestStateString(t *testing.T) {
	for state, want := range map[State]string{
		StateStarting: "starting",
		StateRunning:  "running",
		StateDraining: "draining",
		State(99):     "unknown",
	} {
		if got := state.String(); got != want {
			t.Errorf("State(%d).String() = %q, want %q", state, got, want)
		}
	}
}
