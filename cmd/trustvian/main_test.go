package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureOutput redirects os.Stdout/os.Stderr for the duration of fn,
// returning what was written to each plus fn's return value. Tests using
// this must not run in parallel with each other (os.Stdout/os.Stderr are
// process-global).
func captureOutput(t *testing.T, fn func() int) (stdout, stderr string, exitCode int) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	exitCode = fn()

	os.Stdout, os.Stderr = origOut, origErr
	outW.Close()
	errW.Close()

	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)
	return string(outBytes), string(errBytes), exitCode
}

func TestRunNoArgsShowsUsage(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int { return run(nil) })

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Fatalf("stderr = %q, want it to contain usage text", stderr)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	_, stderr, code := captureOutput(t, func() int { return run([]string{"bogus"}) })

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, `unknown command "bogus"`) {
		t.Fatalf("stderr = %q, want it to mention the unknown command", stderr)
	}
}

func TestRunHelp(t *testing.T) {
	stdout, _, code := captureOutput(t, func() int { return run([]string{"help"}) })

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "trustvian analyze") {
		t.Fatalf("stdout = %q, want it to describe the analyze command", stdout)
	}
}

func TestRunAnalyzeNormalEventIsAllowed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "testdata/normal.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Trustvian Behavioral Analysis") {
		t.Fatalf("stdout missing report header:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Service: svc-payment") {
		t.Fatalf("stdout missing Service line:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Decision: ALLOW") {
		t.Fatalf("stdout = %q, want Decision: ALLOW for a benign first-ever event from a trusted identity", stdout)
	}
}

func TestRunAnalyzeAnomalousEventIsBlocked(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "testdata/anomalous.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Decision: BLOCK") {
		t.Fatalf("stdout = %q, want Decision: BLOCK for a low-identity-confidence external call", stdout)
	}
	if !strings.Contains(stdout, "Detected:") {
		t.Fatalf("stdout = %q, want a Detected section listing contributing signals", stdout)
	}
}

func TestRunAnalyzeMissingFileArg(t *testing.T) {
	_, stderr, code := captureOutput(t, func() int { return run([]string{"analyze"}) })

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero when no file argument is given")
	}
	if !strings.Contains(stderr, "usage") {
		t.Fatalf("stderr = %q, want a usage message", stderr)
	}
}

func TestRunAnalyzeMissingFile(t *testing.T) {
	_, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "testdata/does-not-exist.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a missing file")
	}
	if stderr == "" {
		t.Fatalf("stderr is empty, want an error message")
	}
}

func TestRunAnalyzeMalformedJSON(t *testing.T) {
	_, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "testdata/malformed.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for malformed JSON")
	}
	if !strings.Contains(stderr, "parse") {
		t.Fatalf("stderr = %q, want it to mention a parse error", stderr)
	}
}

func TestRunAnalyzeInvalidEvent(t *testing.T) {
	_, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "testdata/invalid_event.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an event missing required fields")
	}
	if stderr == "" {
		t.Fatalf("stderr is empty, want a validation error message")
	}
}

func TestRunBaselineBuildSummary(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"baseline", "build", "testdata/corpus.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Events processed: 4") {
		t.Fatalf("stdout = %q, want 4 events processed", stdout)
	}
	if !strings.Contains(stdout, "Learned:          3") {
		t.Fatalf("stdout = %q, want 3 learned (the 3 benign, matching events)", stdout)
	}
	if !strings.Contains(stdout, "Skipped:          1") {
		t.Fatalf("stdout = %q, want 1 skipped (the BLOCKed attack event)", stdout)
	}
}

func TestRunBaselineMissingBuildSubcommand(t *testing.T) {
	_, stderr, code := captureOutput(t, func() int {
		return run([]string{"baseline", "testdata/corpus.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero when the 'build' subcommand is missing")
	}
	if !strings.Contains(stderr, "usage") {
		t.Fatalf("stderr = %q, want a usage message", stderr)
	}
}
