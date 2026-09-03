package anomaly_test

import (
	"testing"
	"time"

	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

// BenchmarkScoreKnownFamiliar is the common case: a mature fingerprint
// behaving exactly as expected, so no signal fires. This is the path that
// runs on every normal event and should be the cheapest.
func BenchmarkScoreKnownFamiliar(b *testing.B) {
	fp := fingerprint.Compute(stable("payment-db"))
	bl := matureBaseline(fp, 100, 10)
	feat := features.Features{
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{HasLatency: true, Latency: 10 * time.Millisecond},
	}
	cfg := anomaly.DefaultConfig()

	b.ReportAllocs()
	for b.Loop() {
		_ = anomaly.Score(feat, fp, bl, cfg)
	}
}

// BenchmarkScoreNovelWithAllSignals is the worst case: every signal
// fires (novel fingerprint, latency deviation is moot since baseline is
// empty, error against no history, sensitive target floor).
func BenchmarkScoreNovelWithAllSignals(b *testing.B) {
	empty := matureBaseline(fingerprint.Compute(stable("unrelated")), 0, 0)
	feat := features.Features{
		Stable:   stable("secrets-manager"),
		Volatile: features.VolatileFeatures{Error: true},
	}
	fp := fingerprint.Compute(feat.Stable)
	cfg := anomaly.DefaultConfig()
	cfg.SensitiveTargetFloor = map[string]float64{"secrets-manager": 0.7}

	b.ReportAllocs()
	for b.Loop() {
		_ = anomaly.Score(feat, fp, empty, cfg)
	}
}
