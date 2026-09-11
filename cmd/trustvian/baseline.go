package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

const baselineUsage = "usage: trustvian baseline build [--config <path>] <events.json>"

func runBaseline(args []string) error {
	if len(args) == 0 || args[0] != "build" {
		return fmt.Errorf("%s", baselineUsage)
	}
	return runBaselineBuild(args[1:])
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
func runBaselineBuild(args []string) error {
	fs := flag.NewFlagSet("baseline build", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "path to a Trustvian policy config file (schema v1 YAML)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%s", baselineUsage)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("%s", baselineUsage)
	}

	events, err := loadEvents(rest[0])
	if err != nil {
		return err
	}

	engine, err := newEngine(*configPath)
	if err != nil {
		return err
	}
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
