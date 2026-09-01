package main

import (
	"context"
	"fmt"
	"os"
)

func runBaseline(args []string) error {
	if len(args) != 2 || args[0] != "build" {
		return fmt.Errorf("usage: trustvian baseline build <events.json>")
	}
	return runBaselineBuild(args[1])
}

// runBaselineBuild replays a corpus of events through Analyze+Observe, in
// order, and prints a summary of how many were learned from.
//
// This is gated the same way live traffic is (see Engine.Observe): an
// event whose Decision indicates it was held or stopped is not learned
// from, even during a deliberate baseline-building run. A corpus assumed
// to be entirely trustworthy still benefits from this — it costs nothing
// when every event is in fact benign, and it is one less thing an
// operator has to get right when it is not.
//
// The engine's in-memory Store does not persist beyond this process (see
// internal/store), so this command's result is only visible within its
// own single run — a documented MVP limitation, not a bug: a real
// deployment builds its baseline once, in the long-running process that
// then serves Analyze calls, not by piping state between CLI
// invocations.
func runBaselineBuild(path string) error {
	events, err := loadEvents(path)
	if err != nil {
		return err
	}

	engine := newEngine()
	ctx := context.Background()

	var learnedCount, skippedCount int
	for i, ev := range events {
		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			return fmt.Errorf("event %d (%s): %w", i, ev.ID, err)
		}
		learned, err := engine.Observe(ctx, result)
		if err != nil {
			return fmt.Errorf("event %d (%s): observe: %w", i, ev.ID, err)
		}
		if learned {
			learnedCount++
		} else {
			skippedCount++
		}
	}

	fmt.Fprintf(os.Stdout, "Trustvian Baseline Build\n\n")
	fmt.Fprintf(os.Stdout, "Events processed: %d\n", len(events))
	fmt.Fprintf(os.Stdout, "Learned:          %d\n", learnedCount)
	fmt.Fprintf(os.Stdout, "Skipped:          %d (flagged by policy; not learned from)\n", skippedCount)
	return nil
}
