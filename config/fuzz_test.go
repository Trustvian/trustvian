package config_test

import (
	"testing"

	"github.com/Trustvian/trustvian/config"
)

// FuzzLoad's one required invariant: arbitrary bytes must never panic
// Load, regardless of whether they happen to be valid or invalid
// config. This is the standard "parsers are good fuzz targets"
// invariant this task calls for — kept small and single-purpose, not
// expanded into a broader property-based test suite.
func FuzzLoad(f *testing.F) {
	f.Add([]byte(minimalYAML))
	f.Add([]byte(fullYAML))
	f.Add([]byte(""))
	f.Add([]byte("version: v1"))
	f.Add([]byte("{{{{"))
	f.Add([]byte("version: &a [*a]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = config.Load(data)
	})
}
