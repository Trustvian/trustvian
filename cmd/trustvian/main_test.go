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

// TestRunAnalyzeConfigOverridesDefaultDecision is the central
// acceptance test task 021 exists to satisfy: the same event that
// TestRunAnalyzeNormalEventIsAllowed proves resolves to ALLOW under
// the CLI's built-in default policy must resolve to BLOCK once a
// --config file supplying a different policy is given — proving
// --config actually changes engine behavior, not merely that it
// parses without error.
func TestRunAnalyzeConfigOverridesDefaultDecision(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--config", "testdata/policy-block-all.yaml", "testdata/normal.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Decision: BLOCK") {
		t.Fatalf("stdout = %q, want Decision: BLOCK — the configured policy should have overridden the built-in default (which allows this same event)", stdout)
	}
}

// TestRunAnalyzeInvalidConfigFailsClosed proves this task's central
// security invariant: an explicitly supplied config that fails to
// load/compile must produce a non-zero exit and no analysis output —
// never a silent fallback to the default policy. The fixture's
// "default_decison" typo also doubles as the unknown-field-rejection
// regression check: strict decoding (task 020) must survive the CLI
// boundary, not be silently bypassed by how the CLI calls the loader.
func TestRunAnalyzeInvalidConfigFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--config", "testdata/policy-invalid.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an invalid config file")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run when the config fails to load", stdout)
	}
	if !strings.Contains(stderr, "default_decison") {
		t.Fatalf("stderr = %q, want it to identify the unrecognized field", stderr)
	}
}

// TestRunAnalyzeMissingConfigFailsClosed proves a missing --config
// path is treated the same way as an invalid one: non-zero exit, a
// clear error, and no analysis performed — not a silent fallback and
// not auto-discovery of some other file.
func TestRunAnalyzeMissingConfigFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--config", "testdata/does-not-exist.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a missing config file")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run when the config file is missing", stdout)
	}
	if stderr == "" {
		t.Fatalf("stderr is empty, want an error message")
	}
}

// TestRunBaselineBuildAcceptsConfigFlag proves --config is wired
// through baseline build's own engine construction too, not only
// analyze's — both share the same newEngine helper, and a regression
// that broke one without the other would otherwise go unnoticed.
func TestRunBaselineBuildAcceptsConfigFlag(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"baseline", "build", "--config", "testdata/policy-block-all.yaml", "testdata/corpus.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	// Every event in the corpus is now BLOCKed by the configured
	// catch-all policy, and BLOCK is not eligible for learning (see
	// Engine.Observe) — so nothing should be learned, unlike the
	// corpus's default-policy behavior in TestRunBaselineBuildSummary.
	if !strings.Contains(stdout, "Learned:          0") {
		t.Fatalf("stdout = %q, want 0 learned — the configured block-all policy makes every event ineligible for learning", stdout)
	}
}

// TestRunAnalyzeAcceptsAnomalyConfigFlag is task 033's own CLI-wiring
// proof, mirroring TestRunBaselineBuildAcceptsConfigFlag's shape for
// --anomaly-config: a valid anomaly config file is accepted and
// analysis proceeds normally — this fixture's own weights
// (delegation/n-gram) don't fire for a plain, non-agent event, so the
// point here is that the flag is wired at all, not that it changes
// this specific event's Decision.
func TestRunAnalyzeAcceptsAnomalyConfigFlag(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--anomaly-config", "testdata/anomaly-valid.yaml", "testdata/normal.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Decision:") {
		t.Fatalf("stdout = %q, want a rendered report", stdout)
	}
}

// TestRunAnalyzeInvalidAnomalyConfigFailsClosed mirrors
// TestRunAnalyzeInvalidConfigFailsClosed for --anomaly-config: an
// explicitly supplied anomaly config that fails to load/compile must
// produce a non-zero exit and no analysis output.
func TestRunAnalyzeInvalidAnomalyConfigFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--anomaly-config", "testdata/anomaly-invalid.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an invalid anomaly config file")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run when the anomaly config fails to load", stdout)
	}
	if !strings.Contains(stderr, "delegaton_weight") {
		t.Fatalf("stderr = %q, want it to identify the unrecognized field", stderr)
	}
}

// TestRunAnalyzeMissingAnomalyConfigFailsClosed mirrors
// TestRunAnalyzeMissingConfigFailsClosed for --anomaly-config.
func TestRunAnalyzeMissingAnomalyConfigFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--anomaly-config", "testdata/does-not-exist.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a missing anomaly config file")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run when the anomaly config file is missing", stdout)
	}
	if stderr == "" {
		t.Fatalf("stderr is empty, want an error message")
	}
}

// TestRunBaselineBuildAcceptsAnomalyConfigFlag mirrors
// TestRunBaselineBuildAcceptsConfigFlag for --anomaly-config: both
// subcommands share the same newEngine helper, and a regression that
// wired the flag into one but not the other would otherwise go
// unnoticed.
func TestRunBaselineBuildAcceptsAnomalyConfigFlag(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"baseline", "build", "--anomaly-config", "testdata/anomaly-valid.yaml", "testdata/corpus.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Events processed:") {
		t.Fatalf("stdout = %q, want a rendered summary", stdout)
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
