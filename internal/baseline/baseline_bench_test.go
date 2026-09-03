package baseline_test

import (
	"testing"
	"time"

	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
)

// BenchmarkObserve measures the copy-on-write update cost: every call
// allocates a new Fingerprints map (see Baseline.Observe's doc comment),
// so this is the cost that matters as an actor's known-fingerprint set
// grows, not just the EWMA arithmetic itself.
func BenchmarkObserve(b *testing.B) {
	fp := testFingerprint()
	vol := features.VolatileFeatures{HasLatency: true, Latency: 10 * time.Millisecond}
	bl := baseline.New(testKey)
	now := time.Now()

	b.ReportAllocs()
	for b.Loop() {
		bl = bl.Observe(fp, vol, now)
	}
}
