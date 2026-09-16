package main

import (
	"strings"
	"testing"
)

// TestRunVersionReportsBuildFacts pins that the command prints something
// identifying rather than a placeholder. The exact version string depends
// on how the test binary was built (`go test` yields "(devel)"), so the
// assertion is on the fields that are always present.
func TestRunVersionReportsBuildFacts(t *testing.T) {
	var out strings.Builder
	if err := runVersion(&out, nil); err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{"trustvian", "go:", "platform:"} {
		if !strings.Contains(got, want) {
			t.Errorf("version output missing %q:\n%s", want, got)
		}
	}
	// Never fabricate facts the binary does not have.
	if strings.Contains(got, "unknown") && !strings.Contains(got, "trustvian (unknown)") {
		t.Errorf("version output invented an unknown field:\n%s", got)
	}
}

func TestRunVersionRejectsArguments(t *testing.T) {
	var out strings.Builder
	if err := runVersion(&out, []string{"extra"}); err == nil {
		t.Error("runVersion() error = nil for unexpected arguments, want an error")
	}
}

// TestRunVersionViaDispatch proves the subcommand is actually wired into
// main's dispatch — a version command nobody can invoke is not a feature.
func TestRunVersionViaDispatch(t *testing.T) {
	for _, arg := range []string{"version", "--version", "-v"} {
		stdout, stderr, code := captureOutput(t, func() int { return run([]string{arg}) })
		if code != 0 {
			t.Errorf("run(%q) exit = %d, want 0; stderr = %q", arg, code, stderr)
		}
		if !strings.Contains(stdout, "trustvian") {
			t.Errorf("run(%q) stdout = %q, want version output", arg, stdout)
		}
	}
}
