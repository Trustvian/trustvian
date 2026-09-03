package trust_test

import (
	"testing"

	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/trust"
)

func BenchmarkCompute(b *testing.B) {
	an := anomaly.Anomaly{Score: 0.42, Confidence: 0.8}
	cfg := trust.DefaultConfig()

	b.ReportAllocs()
	for b.Loop() {
		_ = trust.Compute(an, 0.9, 0.1, cfg)
	}
}
