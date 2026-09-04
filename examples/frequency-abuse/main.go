// Command frequency-abuse demonstrates the frequency_deviation anomaly
// signal (docs/tasks/004-anomaly.md), the one scenario in examples/ with
// no existing docs/use-cases.md section to port, since that signal did
// not exist when use-cases.md was written.
//
// A service polls the same status endpoint on a consistent ~10-second
// cadence 25 times, which matures the baseline's IntervalMean. All
// Timestamps are constructed explicitly rather than produced by real
// time.Sleep calls, so the example runs instantly and deterministically.
// One final event arrives only 50ms after the previous one — a burst far
// outside the learned cadence — and frequency_deviation appears in the
// result's Anomaly.Contributors even though the fingerprint itself is
// now fully familiar (categorical_novelty has decayed to zero).
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/event"
)

const (
	pollInterval = 10 * time.Second
	burstGap     = 50 * time.Millisecond
	pollCount    = 25
)

func pollEvent(id string, ts time.Time) event.Event {
	return event.Event{
		ID:        id,
		Timestamp: ts,
		Actor: event.Actor{
			ID:                 "svc-poller",
			Type:               event.ActorTypeService,
			IdentityConfidence: 0.95,
		},
		Operation: event.Operation{
			Category: event.OperationCategoryRPC,
			Name:     "StatusService.GetStatus",
		},
		Target:     event.Target{Name: "status-service"},
		Context:    event.Context{Environment: "production"},
		Attributes: map[string]any{"duration_ms": 20},
	}
}

func main() {
	engine := trustvian.NewEngine()
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ts := base
	var last event.Event

	for i := 1; i <= pollCount; i++ {
		ts = base.Add(time.Duration(i-1) * pollInterval)
		ev := pollEvent(fmt.Sprintf("poll-%d", i), ts)
		last = ev

		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			log.Fatalf("analyze poll %d: %v", i, err)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			log.Fatalf("observe poll %d: %v", i, err)
		}
		if i == 1 || i == 10 || i == 20 || i == pollCount {
			fmt.Printf("poll %2d: confidence=%.2f trust=%.2f decision=%s learned=%v\n",
				i, result.Anomaly.Confidence, result.Trust.Score, result.Decision, learned)
		}
	}

	burst := pollEvent("burst", last.Timestamp.Add(burstGap))

	result, err := engine.Analyze(ctx, burst)
	if err != nil {
		log.Fatalf("analyze burst: %v", err)
	}

	fmt.Println()
	fmt.Println(result.Explain())

	found := false
	for _, c := range result.Anomaly.Contributors {
		if c.Name == "frequency_deviation" {
			found = true
		}
	}
	if !found {
		log.Fatal("expected frequency_deviation in result.Anomaly.Contributors, it was not present")
	}
	fmt.Println("frequency_deviation signal present in Contributors, as expected.")
}
