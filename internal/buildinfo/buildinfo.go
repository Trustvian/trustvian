// Package buildinfo reports how the running binary was built, for
// `trustvian version` and anything else that needs to identify an
// artifact.
//
// Everything here is read from runtime/debug.ReadBuildInfo rather than
// injected at link time. Go already records the module version for a
// binary installed with `go install module@version`, and records
// vcs.revision/vcs.time/vcs.modified for a binary built from a checkout —
// so an -ldflags stamping contract would add a build-time coupling to
// re-derive facts the toolchain already provides, and would silently
// produce "unknown" for anyone who built without the magic flags.
//
// It also keeps this package free of the package-level mutable state
// .claude/rules/go.md forbids: there is no var to overwrite, only a
// function that reads what the runtime already holds.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Info describes one built binary. Every field is best-effort: a binary
// built in a way that records nothing (for example `go build` against a
// source tree with no VCS metadata) still yields a usable value, with the
// unknown parts empty rather than fabricated.
type Info struct {
	// Version is the module version — a real semantic version for an
	// installed release, a pseudo-version for a commit build, or "(devel)"
	// when built from a working tree.
	Version string

	// Revision is the VCS commit the binary was built from, empty when the
	// build recorded none.
	Revision string

	// Time is the commit timestamp, in RFC 3339, empty when unrecorded.
	// Deliberately the *commit* time rather than the build time: build time
	// differs between two builds of identical source, which would make
	// otherwise-identical artifacts look different.
	Time string

	// Modified reports whether the working tree had uncommitted changes.
	// This is the field that distinguishes a reproducible artifact from a
	// developer's local build, which matters when someone reports a bug
	// against "v0.8.0".
	Modified bool

	GoVersion string
	OS        string
	Arch      string
}

// Read returns the running binary's build information.
func Read() Info {
	info := Info{
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		// Only happens for a binary built without module support at all.
		return info
	}
	info.Version = bi.Main.Version

	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.time":
			info.Time = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// String renders the information as the `trustvian version` output: one
// line per fact, with unknown fields omitted rather than printed as
// "unknown", so the output never implies knowledge the binary does not
// have.
func (i Info) String() string {
	var b strings.Builder

	version := i.Version
	if version == "" {
		version = "(unknown)"
	}
	fmt.Fprintf(&b, "trustvian %s\n", version)

	if i.Revision != "" {
		revision := i.Revision
		if i.Modified {
			// A dirty build is not the commit it claims to be; say so where
			// it cannot be missed.
			revision += " (modified)"
		}
		fmt.Fprintf(&b, "  revision:  %s\n", revision)
	}
	if i.Time != "" {
		fmt.Fprintf(&b, "  committed: %s\n", i.Time)
	}
	fmt.Fprintf(&b, "  go:        %s\n", i.GoVersion)
	fmt.Fprintf(&b, "  platform:  %s/%s\n", i.OS, i.Arch)

	return b.String()
}
