package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	trustvian "github.com/Trustvian/trustvian"
)

const analyzeUsage = "usage: trustvian analyze [--config <path>] [--anomaly-config <path>] <events.json>"

func runAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", "", "path to a Trustvian policy config file (schema v1 YAML)")
	anomalyConfigPath := fs.String("anomaly-config", "", "path to a Trustvian anomaly config file (schema v1 YAML)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%s", analyzeUsage)
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("%s", analyzeUsage)
	}

	events, err := loadEvents(rest[0])
	if err != nil {
		return err
	}

	engine, err := newEngine(*configPath, *anomalyConfigPath)
	if err != nil {
		return err
	}
	ctx := context.Background()

	for i, ev := range events {
		result, err := engine.Analyze(ctx, ev)
		if err != nil {
			return fmt.Errorf("event %d (%s): %w", i, ev.ID, err)
		}
		printReport(os.Stdout, result)
	}
	return nil
}

// printReport renders a Result in the shape described in the Trustvian
// project spec's CLI example: a header block of scores, the signals that
// contributed to the anomaly score, and the final decision.
func printReport(w io.Writer, result trustvian.Result) {
	fmt.Fprintln(w, "Trustvian Behavioral Analysis")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Service: %s\n", result.Event.Actor.ID)
	fmt.Fprintf(w, "Anomaly: %.2f\n", result.Anomaly.Score)
	fmt.Fprintf(w, "Trust:   %.2f\n", result.Trust.Score)
	fmt.Fprintf(w, "Risk:    %s\n", strings.ToUpper(string(result.Trust.Risk)))
	fmt.Fprintln(w)

	if len(result.Anomaly.Contributors) > 0 {
		fmt.Fprintln(w, "Detected:")
		for _, c := range result.Anomaly.Contributors {
			fmt.Fprintf(w, "  ! %s\n", c.Detail)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Decision: %s\n", strings.ToUpper(string(result.Decision)))
	fmt.Fprintf(w, "Reason:   %s\n", result.Explanation.Reason)
	fmt.Fprintln(w)
}
