package anomaly_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/baseline"
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
		Stable: stable("payment-db"),
		Volatile: features.VolatileFeatures{
			HasLatency: true, Latency: 10 * time.Millisecond,
			Timestamp: bl.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: frequency_deviation must not fire here
		},
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

// BenchmarkScoreTransitionDeviation measures the v0.6 foundation's own
// added cost on a mature, otherwise-unremarkable fingerprint whose
// PredecessorCounts map holds a realistic number of distinct entries
// (near maxPredecessors) — the worst case for the map lookup
// transitionSignal performs, not the empty-map case
// BenchmarkScoreKnownFamiliar's self-transition already exercises
// cheaply.
func BenchmarkScoreTransitionDeviation(b *testing.B) {
	fp := fingerprint.Compute(stable("payment-db"))
	bl := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 60 { // near maxPredecessors (64), without exceeding it
		pred := fingerprint.Compute(stable(fmt.Sprintf("predecessor-%d", i)))
		bl = bl.Observe(pred, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
		bl = bl.Observe(fp, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
	}
	// The predecessor for the event under test: never before observed
	// leading to fp, so transition_deviation actually fires.
	unseenPredecessor := fingerprint.Compute(stable("unseen-predecessor"))
	bl = bl.Observe(unseenPredecessor, features.VolatileFeatures{}, now)
	now = now.Add(time.Second)

	feat := features.Features{Stable: stable("payment-db"), Volatile: features.VolatileFeatures{Timestamp: now}}
	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7

	b.ReportAllocs()
	for b.Loop() {
		_ = anomaly.Score(feat, fp, bl, cfg)
	}
}
