// Command trustvian is the Trustvian CLI: a developer-friendly way to
// run the behavioral engine against a file of events without writing Go.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage(os.Stderr)
		return 2
	}

	var err error
	switch args[0] {
	case "analyze":
		err = runAnalyze(args[1:])
	case "baseline":
		err = runBaseline(args[1:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "trustvian: unknown command %q\n\n", args[0])
		usage(os.Stderr)
		return 2
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "trustvian: %v\n", err)
		return 1
	}
	return 0
}

func usage(w *os.File) {
	fmt.Fprintln(w, `Trustvian - behavioral security and trust engine

Usage:
  trustvian analyze <events.json>        Score each event and print a report
  trustvian baseline build <events.json> Learn a baseline from a corpus of events

<events.json> is a JSON array of events, e.g.:
  [{"id":"evt-1","timestamp":"2026-01-01T12:00:00Z","actor":{"id":"svc-payment","type":"service","identity_confidence":0.95},"operation":{"category":"http","name":"POST /payment"},"target":{"name":"payment-db"},"context":{"environment":"production"}}]`)
}
