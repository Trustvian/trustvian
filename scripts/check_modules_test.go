// Package scripts holds repository tooling. This file tests
// check-modules.sh, because that script gates every pull request and its
// failure modes are not obvious from reading it.
//
// It exists because of a real regression: the script verified "the
// declared root version exists" by looking for a local git tag, which
// passed on a developer's full clone and failed in CI, where
// actions/checkout fetches no tags by default. The check reported that
// v0.8.0 "is not an existing tag" — about a version that very much exists.
// The tests below pin the distinction between a version that is missing
// and a checkout that simply cannot see it.
package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a throwaway repository tree with the given go.mod
// contents and returns its path. Local fixtures rather than a clone of the
// real repository: these tests must not depend on network access or on
// which tags a developer happens to have fetched.
func fixture(t *testing.T, rootMod, nestedMod string) string {
	t.Helper()

	dir := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	write("go.mod", rootMod)
	if nestedMod != "" {
		write("nested/go.mod", nestedMod)
	}

	// The script lives at <repo>/scripts/, and locates the repository by
	// going up one level from itself.
	src, err := os.ReadFile("check-modules.sh")
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	write("scripts/check-modules.sh", string(src))
	if err := os.Chmod(filepath.Join(dir, "scripts/check-modules.sh"), 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return dir
}

// run executes the script inside a fixture and reports success plus output.
// offline forbids the module-proxy fallback, which is how these tests stay
// deterministic without network access.
func run(t *testing.T, dir string, offline bool) (bool, string) {
	t.Helper()

	cmd := exec.Command("./scripts/check-modules.sh")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir))
	if offline {
		cmd.Env = append(cmd.Env, "CHECK_MODULES_OFFLINE=1")
	}
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}

const validRoot = "module github.com/Trustvian/trustvian\n\ngo 1.27\n"

func TestValidDevelopmentStatePasses(t *testing.T) {
	// The repository's actual shape: a canonical root with no replace, and a
	// repository-internal nested module replaced to the root. The zero
	// placeholder needs no version lookup, so this holds offline.
	dir := fixture(t, validRoot,
		"module trustvian-nested\n\ngo 1.27\n\n"+
			"replace github.com/Trustvian/trustvian => ../\n\n"+
			"require github.com/Trustvian/trustvian v0.0.0-00010101000000-000000000000\n")

	ok, out := run(t, dir, true)
	if !ok {
		t.Errorf("valid development state failed:\n%s", out)
	}
}

func TestReplaceInPublishedRootFails(t *testing.T) {
	// A replace in the published module's own go.mod is ignored by
	// consumers, so the module would build differently for everyone else.
	dir := fixture(t, validRoot+"\nreplace example.com/x => ./x\n", "")

	ok, out := run(t, dir, true)
	if ok {
		t.Errorf("a replace in the published root module was accepted:\n%s", out)
	}
	if !strings.Contains(out, "replace") {
		t.Errorf("failure did not mention the replace:\n%s", out)
	}
}

func TestResolvablePathWithLocalReplaceFails(t *testing.T) {
	// Premature promotion: once the path is resolvable, a local replace
	// makes it a module nobody else can build.
	dir := fixture(t, validRoot,
		"module github.com/Trustvian/trustvian/nested\n\ngo 1.27\n\n"+
			"replace github.com/Trustvian/trustvian => ../\n\n"+
			"require github.com/Trustvian/trustvian v0.0.0-00010101000000-000000000000\n")

	ok, out := run(t, dir, true)
	if ok {
		t.Errorf("resolvable module path with a local replace was accepted:\n%s", out)
	}
}

func TestReplaceToForeignPathFails(t *testing.T) {
	// A replace pointing anywhere but the repository root builds only on
	// the machine that wrote it.
	dir := fixture(t, validRoot,
		"module trustvian-nested\n\ngo 1.27\n\n"+
			"replace github.com/Trustvian/trustvian => /somewhere/else\n")

	if ok, out := run(t, dir, true); ok {
		t.Errorf("a replace to a foreign path was accepted:\n%s", out)
	}
}

// TestUnverifiableVersionFailsExplicitly is the regression for the CI
// break. A checkout with no tags cannot confirm a version locally; with the
// proxy also unavailable the script must say so — and must not pass.
//
// Passing here would be the dangerous outcome: it would silently disable
// the version check on exactly the shallow checkouts CI uses.
func TestUnverifiableVersionFailsExplicitly(t *testing.T) {
	dir := fixture(t, validRoot,
		"module trustvian-nested\n\ngo 1.27\n\n"+
			"replace github.com/Trustvian/trustvian => ../\n\n"+
			"require github.com/Trustvian/trustvian v0.8.0\n")

	ok, out := run(t, dir, true)
	if ok {
		t.Errorf("an unverifiable version was silently accepted:\n%s", out)
	}
	// The message must point at the cause, not blame the version.
	if !strings.Contains(out, "cannot be verified") {
		t.Errorf("failure did not explain that verification was impossible:\n%s", out)
	}
	if strings.Contains(out, "does not exist as a tag or a published module version") {
		t.Errorf("failure wrongly claimed the version does not exist:\n%s", out)
	}
}

// TestSingleLineRequireIsParsed guards a bug this script already had once:
// only the `require ( ... )` block form was parsed, so a single-line
// `require` silently skipped the version rule entirely.
func TestSingleLineRequireIsParsed(t *testing.T) {
	dir := fixture(t, validRoot,
		"module trustvian-nested\n\ngo 1.27\n\n"+
			"replace github.com/Trustvian/trustvian => ../\n\n"+
			"require github.com/Trustvian/trustvian v0.8.0\n")

	_, out := run(t, dir, true)
	if !strings.Contains(out, "v0.8.0") {
		t.Errorf("single-line require was not parsed; the version rule was skipped:\n%s", out)
	}
}
