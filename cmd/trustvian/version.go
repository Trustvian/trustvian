package main

import (
	"fmt"
	"io"

	"github.com/trustvian/trustvian/internal/buildinfo"
)

const versionUsage = "usage: trustvian version"

// runVersion prints how this binary was built, so a bug report or a
// deployed artifact can be traced back to an exact commit. See
// internal/buildinfo for why the facts come from Go's own build
// information rather than link-time flags.
func runVersion(w io.Writer, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("%s", versionUsage)
	}
	fmt.Fprint(w, buildinfo.Read())
	return nil
}
