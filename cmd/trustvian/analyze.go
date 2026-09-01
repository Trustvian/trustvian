package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	trustvian "github.com/Trustvian/trustvian"
)

func runAnalyze(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: trustvian analyze <events.json>")
	}

	events, err := loadEvents(args[0])
	if err != nil {
		return err
	}

	engine := newEngine()
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
