package config_test

import (
	"testing"

	"github.com/Trustvian/trustvian/config"
)

// BenchmarkCompileAlerts measures CompileAlerts's cost on a
// moderately-sized, realistic config — the AlertConfig analogue of
// BenchmarkCompilePolicy. Startup-path work, run once per Engine/Sink
// wiring, not per Result — see docs/tasks/023-declarative-alert-configuration.md
// § Benchmarks.
func BenchmarkCompileAlerts(b *testing.B) {
	cfg := fullValidAlertConfig()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := config.CompileAlerts(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateAlerts isolates AlertConfig.Validate's own cost
// from compilation.
func BenchmarkValidateAlerts(b *testing.B) {
	cfg := fullValidAlertConfig()

	b.ReportAllocs()
	for b.Loop() {
		if err := cfg.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
