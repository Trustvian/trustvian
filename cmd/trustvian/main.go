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
  trustvian analyze [--config <path>] [--anomaly-config <path>] <events.json>
      Score each event and print a report
  trustvian baseline build [--config <path>] [--anomaly-config <path>] <events.json>
      Learn a baseline from a corpus of events

--config <path> loads a schema-v1 YAML policy config (see config.LoadFile)
and uses it instead of the CLI's built-in default policy. Without it,
behavior is unchanged from before this flag existed.

--anomaly-config <path> loads a schema-v1 YAML anomaly config (see
config.LoadAnomalyFile) and uses it instead of the engine's built-in
default anomaly scoring (anomaly.DefaultConfig) — this is how v0.6/v0.7
signals (transition/n-gram/Markov/delegation deviation, all opt-in and
disabled by default) get enabled from the CLI. Without it, behavior is
unchanged from before this flag existed.

<events.json> is a JSON array of events, e.g.:
  [{"id":"evt-1","timestamp":"2026-01-01T12:00:00Z","actor":{"id":"svc-payment","type":"service","identity_confidence":0.95},"operation":{"category":"http","name":"POST /payment"},"target":{"name":"payment-db"},"context":{"environment":"production"}}]`)
}
