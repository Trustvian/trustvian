package config_test

import (
	"testing"

	"github.com/Trustvian/trustvian/config"
)

// BenchmarkCompilePolicy measures CompilePolicy's cost on a
// moderately-sized, realistic config. This is startup-path work, run
// once per Engine construction, not per Analyze call — see
// docs/tasks/019-policy-config-model.md § Benchmarks for why this is
// measured for the record rather than optimized against.
func BenchmarkCompilePolicy(b *testing.B) {
	cfg := fullValidConfig()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := config.CompilePolicy(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidate isolates Validate's own cost from compilation.
func BenchmarkValidate(b *testing.B) {
	cfg := fullValidConfig()

	b.ReportAllocs()
	for b.Loop() {
		if err := cfg.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
