package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// --- Task 034: --storage-config ---

func TestRunAnalyzeAcceptsStorageConfigFlag(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", "testdata/storage-memory.yaml", "testdata/normal.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Decision:") {
		t.Fatalf("stdout = %q, want a rendered report", stdout)
	}
}

// TestRunAnalyzeInvalidStorageConfigFailsClosed mirrors the equivalent
// --config/--anomaly-config regressions, and matters more here than for
// either of those: silently continuing after a failed *storage* request
// would discard learned state rather than merely scoring differently.
func TestRunAnalyzeInvalidStorageConfigFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", "testdata/storage-invalid.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an invalid storage config file")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run when the storage config fails to load", stdout)
	}
	if !strings.Contains(stderr, "typ") {
		t.Fatalf("stderr = %q, want it to identify the unrecognized field", stderr)
	}
}

func TestRunAnalyzeMissingStorageConfigFailsClosed(t *testing.T) {
	stdout, _, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", "testdata/does-not-exist.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a missing storage config file")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
}

// TestRunAnalyzeIncompleteStorageConfigFailsClosed is the CLI-level half
// of config.CompileStorage's own fail-closed guarantee for a
// *misconfigured* backend: storage-postgres.yaml names type: postgres
// but supplies no DSN, so the command must abort rather than run on a
// silently substituted in-memory store.
//
// Before task 035 this fixture proved something different — that
// postgres was recognized but unimplemented. That state no longer
// exists, so the assertion moved to the invariant that outlasted it:
// configuration this CLI cannot honor stops the command.
func TestRunAnalyzeIncompleteStorageConfigFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", "testdata/storage-postgres.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for an incomplete storage config")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run on a store that could not be built", stdout)
	}
	if !strings.Contains(stderr, "requires a dsn") {
		t.Fatalf("stderr = %q, want it to name the missing dsn", stderr)
	}
}

// TestRunAnalyzeUnreachablePostgresFailsClosed is the invariant task 035
// exists to protect, asserted at the outermost layer a user touches: an
// operator who asks for PostgreSQL and cannot get PostgreSQL gets an
// error, never a quiet downgrade to in-memory storage. A fallback here
// would mean every decision that followed was made against state the
// operator believed was durable and shared, and none of it would
// survive the process — silent persistence downgrade, which
// docs/SECURITY.md prohibits outright.
//
// This test needs no database: it depends on PostgreSQL being *absent*,
// which is exactly what a developer machine with no container running
// provides, so it runs everywhere rather than being DSN-gated.
func TestRunAnalyzeUnreachablePostgresFailsClosed(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", "testdata/storage-postgres-unreachable.yaml", "testdata/normal.json"})
	})

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero when PostgreSQL is unreachable")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty — no analysis must run against a store that could not be reached", stdout)
	}
	if !strings.Contains(stderr, "unavailable") {
		t.Fatalf("stderr = %q, want it to report the database as unavailable", stderr)
	}
	// The DSN carries a password. It must not reach the terminal.
	if strings.Contains(stderr, "unused") {
		t.Fatalf("stderr leaked the DSN password: %q", stderr)
	}
}

// TestBaselineBuildThenAnalyzePersistsAcrossCommands is task 034's
// central end-to-end proof, and the first time this CLI can demonstrate
// it at all: `baseline build` writes learned state to a file store, and
// a *separate* `analyze` invocation reads it back.
//
// The assertion targets categorical_novelty's own Detail string, which
// distinguishes the two states precisely — "never observed" on a cold
// store versus "observed N/20 times" once the persisted baseline is
// loaded. Before --storage-config existed, the second run could only
// ever report the former, because nothing survived the first process.
func TestBaselineBuildThenAnalyzePersistsAcrossCommands(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "baseline.json")
	storageCfgPath := filepath.Join(dir, "storage.yaml")
	storageCfg := "version: v1\ntype: file\nfile:\n  path: " + statePath + "\n"
	if err := os.WriteFile(storageCfgPath, []byte(storageCfg), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Cold store: the fingerprint has never been seen.
	coldStdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", storageCfgPath, "testdata/normal.json"})
	})
	if code != 0 {
		t.Fatalf("cold analyze: exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(coldStdout, "never observed") {
		t.Fatalf("cold analyze stdout = %q, want it to report the fingerprint as never observed", coldStdout)
	}

	// Learn from the corpus, persisting to the same file store.
	_, stderr, code = captureOutput(t, func() int {
		return run([]string{"baseline", "build", "--storage-config", storageCfgPath, "testdata/corpus.json"})
	})
	if code != 0 {
		t.Fatalf("baseline build: exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file %s not written: %v", statePath, err)
	}

	// A separate invocation now scores against the persisted baseline.
	warmStdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", storageCfgPath, "testdata/normal.json"})
	})
	if code != 0 {
		t.Fatalf("warm analyze: exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if strings.Contains(warmStdout, "never observed") {
		t.Fatalf("warm analyze stdout = %q, want the persisted baseline to have been loaded — the fingerprint should no longer read as never observed", warmStdout)
	}
	if !strings.Contains(warmStdout, "times required for maturity") {
		t.Fatalf("warm analyze stdout = %q, want it to report partial maturity from the persisted baseline", warmStdout)
	}
}

func TestRunBaselineBuildAcceptsStorageConfigFlag(t *testing.T) {
	stdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"baseline", "build", "--storage-config", "testdata/storage-memory.yaml", "testdata/corpus.json"})
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Events processed:") {
		t.Fatalf("stdout = %q, want a rendered summary", stdout)
	}
}

// TestBaselineBuildThenAnalyzePersistsAcrossCommandsPostgres is the
// PostgreSQL twin of TestBaselineBuildThenAnalyzePersistsAcrossCommands,
// and it is the proof that matters most for task 035: the backend is not
// "done" because its own package tests pass, it is done when an operator
// running two ordinary CLI commands gets learned state carried between
// them. This exercises the whole public path — YAML document →
// config.LoadStorageFile → config.CompileStorage → postgres.Store →
// Engine — with no internal import anywhere in the flow.
//
// DSN-gated: it skips when TRUSTVIAN_TEST_POSTGRES_DSN is unset, so
// `go test ./...` on a machine with no database still passes. See
// docs/storage-guide.md § Running the integration tests.
func TestBaselineBuildThenAnalyzePersistsAcrossCommandsPostgres(t *testing.T) {
	dsn := os.Getenv("TRUSTVIAN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TRUSTVIAN_TEST_POSTGRES_DSN not set; see docs/storage-guide.md")
	}

	// A unique actor per run keys this test's baseline away from any other
	// test sharing the database, so the cold-store assertion below holds
	// without this test truncating a table other tests are using.
	corpus, normal := postgresFixturePair(t)

	dir := t.TempDir()
	storageCfgPath := filepath.Join(dir, "storage.yaml")
	storageCfg := "version: v1\ntype: postgres\npostgres:\n  dsn: " + dsn + "\n"
	if err := os.WriteFile(storageCfgPath, []byte(storageCfg), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	coldStdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", storageCfgPath, normal})
	})
	if code != 0 {
		t.Fatalf("cold analyze: exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if !strings.Contains(coldStdout, "never observed") {
		t.Fatalf("cold analyze stdout = %q, want it to report the fingerprint as never observed", coldStdout)
	}

	_, stderr, code = captureOutput(t, func() int {
		return run([]string{"baseline", "build", "--storage-config", storageCfgPath, corpus})
	})
	if code != 0 {
		t.Fatalf("baseline build: exit code = %d, want 0; stderr = %q", code, stderr)
	}

	warmStdout, stderr, code := captureOutput(t, func() int {
		return run([]string{"analyze", "--storage-config", storageCfgPath, normal})
	})
	if code != 0 {
		t.Fatalf("warm analyze: exit code = %d, want 0; stderr = %q", code, stderr)
	}
	if strings.Contains(warmStdout, "never observed") {
		t.Fatalf("warm analyze stdout = %q, want the persisted baseline to have been loaded from PostgreSQL", warmStdout)
	}
	if !strings.Contains(warmStdout, "times required for maturity") {
		t.Fatalf("warm analyze stdout = %q, want it to report partial maturity from the persisted baseline", warmStdout)
	}
}

// postgresFixturePair copies testdata/corpus.json and testdata/normal.json
// into the test's temp dir with every actor.id rewritten to a value unique
// to this run, and returns the two new paths. Rewriting the fixture rather
// than truncating the table is what lets this test share a database with
// the store package's own integration tests without either interfering
// with the other.
func postgresFixturePair(t *testing.T) (corpus, normal string) {
	t.Helper()

	dir := t.TempDir()
	actor := fmt.Sprintf("cli-e2e-%d-%d", time.Now().UnixNano(), os.Getpid())

	out := make([]string, 0, 2)
	for _, name := range []string{"corpus.json", "normal.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", name, err)
		}
		// The fixtures use a single actor id; swap it for the unique one.
		rewritten := strings.ReplaceAll(string(data), `"id": "svc-payment"`, `"id": "`+actor+`"`)
		if rewritten == string(data) {
			t.Fatalf("fixture %s: actor id placeholder not found — fixture shape changed", name)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(rewritten), 0o600); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
		out = append(out, path)
	}
	return out[0], out[1]
}
