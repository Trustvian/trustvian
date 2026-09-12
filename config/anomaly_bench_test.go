package config_test

import (
	"testing"

	"github.com/Trustvian/trustvian/config"
)

// BenchmarkCompileAnomaly measures CompileAnomaly's cost on a fully
// populated config — startup-path work, run once per Engine
// construction (see trustvian.WithAnomalyConfig), never per Analyze
// call. Compare against BenchmarkCompilePolicy: both are one-time
// setup costs this task's own brief required stay off the hot path
// (§40 — "compile once, not per event").
func BenchmarkCompileAnomaly(b *testing.B) {
	cfg := fullValidAnomalyConfig()

	b.ReportAllocs()
	for b.Loop() {
		if _, err := config.CompileAnomaly(cfg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateAnomaly isolates Validate's own cost from
// compilation.
func BenchmarkValidateAnomaly(b *testing.B) {
	cfg := fullValidAnomalyConfig()

	b.ReportAllocs()
	for b.Loop() {
		if err := cfg.Validate(); err != nil {
			b.Fatal(err)
		}
	}
}
