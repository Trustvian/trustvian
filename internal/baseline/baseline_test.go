package baseline_test

import (
	"math"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

var testKey = baseline.Key{ActorID: "svc-payment", Environment: "production"}

func testFingerprint() fingerprint.Fingerprint {
	return fingerprint.Compute(features.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryHTTP,
		OperationName:     "POST /payment",
		TargetName:        "payment-db",
		Environment:       "production",
	})
}

func TestBaselineObserveTracksMaturityCount(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	const observations = 25
	const maturityThreshold = 20

	for i := 1; i <= observations; i++ {
		b = b.Observe(fp, features.VolatileFeatures{}, time.Now())

		stats, ok := b.Fingerprints[fp.ID]
		if !ok {
			t.Fatalf("observation %d: fingerprint missing from baseline", i)
		}
		if stats.Count != uint64(i) {
			t.Fatalf("observation %d: Count = %d, want %d", i, stats.Count, i)
		}

		wantMature := i >= maturityThreshold
		gotMature := stats.Count >= maturityThreshold
		if gotMature != wantMature {
			t.Fatalf("observation %d: maturity (Count>=%d) = %v, want %v", i, maturityThreshold, gotMature, wantMature)
		}
	}
}

func TestBaselineObserveIsImmutable(t *testing.T) {
	fp := testFingerprint()
	before := baseline.New(testKey)

	after := before.Observe(fp, features.VolatileFeatures{}, time.Now())

	if len(before.Fingerprints) != 0 {
		t.Fatalf("Observe mutated the receiver: before.Fingerprints = %v, want empty", before.Fingerprints)
	}
	if len(after.Fingerprints) != 1 {
		t.Fatalf("after.Fingerprints has %d entries, want 1", len(after.Fingerprints))
	}

	// A second Observe on `after` must not reach back and affect the
	// first snapshot either.
	again := after.Observe(fp, features.VolatileFeatures{}, time.Now())
	if after.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("second Observe mutated an earlier snapshot: Count = %d, want 1", after.Fingerprints[fp.ID].Count)
	}
	if again.Fingerprints[fp.ID].Count != 2 {
		t.Fatalf("again.Fingerprints[fp.ID].Count = %d, want 2", again.Fingerprints[fp.ID].Count)
	}
}

func TestFingerprintStatsColdStart(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	b = b.Observe(fp, features.VolatileFeatures{HasLatency: true, Latency: 100 * time.Millisecond, Error: true}, time.Now())

	stats := b.Fingerprints[fp.ID]
	if stats.Count != 1 {
		t.Fatalf("Count = %d, want 1", stats.Count)
	}
	if stats.LatencyObservations != 1 {
		t.Fatalf("LatencyObservations = %d, want 1", stats.LatencyObservations)
	}
	if stats.LatencyMeanDuration() != 100*time.Millisecond {
		t.Fatalf("LatencyMeanDuration() = %v, want 100ms", stats.LatencyMeanDuration())
	}
	if stats.LatencyStdDevDuration() != 0 {
		t.Fatalf("LatencyStdDevDuration() = %v, want 0 on first observation", stats.LatencyStdDevDuration())
	}
	if stats.ErrorRate != 1.0 {
		t.Fatalf("ErrorRate = %v, want 1.0 on first (errored) observation", stats.ErrorRate)
	}
}

func TestFingerprintStatsLatencyConvergesToStableValue(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	const target = 100 * time.Millisecond
	for range 200 {
		b = b.Observe(fp, features.VolatileFeatures{HasLatency: true, Latency: target}, time.Now())
	}

	stats := b.Fingerprints[fp.ID]
	gotMean := stats.LatencyMeanDuration()
	if diff := gotMean - target; diff > time.Microsecond || diff < -time.Microsecond {
		t.Fatalf("LatencyMeanDuration() = %v, want ~%v after repeated identical observations", gotMean, target)
	}
	if stats.LatencyVariance > 1 {
		t.Fatalf("LatencyVariance = %v, want ~0 after repeated identical observations", stats.LatencyVariance)
	}
}

func TestFingerprintStatsIntervalConvergesToStableValue(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	const interval = 10 * time.Second
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{}, now)
		now = now.Add(interval)
	}

	stats := b.Fingerprints[fp.ID]
	if stats.IntervalObservations == 0 {
		t.Fatal("IntervalObservations = 0 after 50 observations, want > 0")
	}
	gotMean := time.Duration(stats.IntervalMean)
	if diff := gotMean - interval; diff < -500*time.Millisecond || diff > 500*time.Millisecond {
		t.Fatalf("IntervalMean = %s after convergence, want ~%s", gotMean, interval)
	}
	if stats.IntervalVariance > float64(time.Second*time.Second) {
		t.Fatalf("IntervalVariance = %v, want ~0 after repeated identical intervals", stats.IntervalVariance)
	}
}

func TestFingerprintStatsIntervalObservationsIsCountMinusOne(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b = b.Observe(fp, features.VolatileFeatures{}, now)

	stats := b.Fingerprints[fp.ID]
	if stats.IntervalObservations != 0 {
		t.Fatalf("IntervalObservations = %d after a single observation, want 0 (no prior LastObserved to measure from)", stats.IntervalObservations)
	}

	now = now.Add(5 * time.Second)
	b = b.Observe(fp, features.VolatileFeatures{}, now)
	stats = b.Fingerprints[fp.ID]
	if stats.IntervalObservations != 1 {
		t.Fatalf("IntervalObservations = %d after a second observation, want 1", stats.IntervalObservations)
	}
	if got := time.Duration(stats.IntervalMean); got != 5*time.Second {
		t.Fatalf("IntervalMean = %s after exactly one interval, want exactly 5s", got)
	}
}

func TestFingerprintStatsSkipsLatencyWhenAbsent(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	b = b.Observe(fp, features.VolatileFeatures{HasLatency: true, Latency: 50 * time.Millisecond}, time.Now())
	b = b.Observe(fp, features.VolatileFeatures{HasLatency: false}, time.Now())

	stats := b.Fingerprints[fp.ID]
	if stats.Count != 2 {
		t.Fatalf("Count = %d, want 2", stats.Count)
	}
	if stats.LatencyObservations != 1 {
		t.Fatalf("LatencyObservations = %d, want 1 (second observation had no latency)", stats.LatencyObservations)
	}
	if stats.LatencyMeanDuration() != 50*time.Millisecond {
		t.Fatalf("LatencyMeanDuration() = %v, want unaffected 50ms", stats.LatencyMeanDuration())
	}
}

func TestFingerprintStatsErrorRateTracksDirection(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{Error: false}, time.Now())
	}
	if rate := b.Fingerprints[fp.ID].ErrorRate; rate > 0.01 {
		t.Fatalf("ErrorRate = %v after 50 clean observations, want ~0", rate)
	}

	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{Error: true}, time.Now())
	}
	if rate := b.Fingerprints[fp.ID].ErrorRate; rate < 0.99 {
		t.Fatalf("ErrorRate = %v after 50 errored observations, want ~1", rate)
	}
}

func TestBaselineObserveDistinctFingerprintsDoNotInterfere(t *testing.T) {
	fpA := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryHTTP,
		OperationName: "POST /payment", TargetName: "payment-db", Environment: "production",
	})
	fpB := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryExternal,
		OperationName: "webhook.send", TargetName: "notify-service", Environment: "production",
	})

	b := baseline.New(testKey)
	b = b.Observe(fpA, features.VolatileFeatures{}, time.Now())
	b = b.Observe(fpA, features.VolatileFeatures{}, time.Now())
	b = b.Observe(fpB, features.VolatileFeatures{}, time.Now())

	if got := b.Fingerprints[fpA.ID].Count; got != 2 {
		t.Fatalf("fpA Count = %d, want 2", got)
	}
	if got := b.Fingerprints[fpB.ID].Count; got != 1 {
		t.Fatalf("fpB Count = %d, want 1", got)
	}
	if len(b.Fingerprints) != 2 {
		t.Fatalf("len(Fingerprints) = %d, want 2", len(b.Fingerprints))
	}
}

func TestFingerprintStatsLatencyStdDevIsNonNegative(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	latencies := []time.Duration{10 * time.Millisecond, 200 * time.Millisecond, 15 * time.Millisecond, 5 * time.Millisecond}
	for _, l := range latencies {
		b = b.Observe(fp, features.VolatileFeatures{HasLatency: true, Latency: l}, time.Now())
	}

	stats := b.Fingerprints[fp.ID]
	if stats.LatencyVariance < 0 {
		t.Fatalf("LatencyVariance = %v, must be non-negative", stats.LatencyVariance)
	}
	if math.IsNaN(stats.LatencyVariance) {
		t.Fatalf("LatencyVariance is NaN")
	}

	stdDev := stats.LatencyStdDevDuration()
	if stdDev < 0 {
		t.Fatalf("LatencyStdDevDuration() = %v, must be non-negative", stdDev)
	}
}

func TestFingerprintStatsIsStale(t *testing.T) {
	fp := testFingerprint()
	b := baseline.New(testKey)

	observedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	b = b.Observe(fp, features.VolatileFeatures{}, observedAt)
	stats := b.Fingerprints[fp.ID]

	tests := []struct {
		name   string
		now    time.Time
		maxAge time.Duration
		want   bool
	}{
		{"well within threshold", observedAt.Add(time.Minute), time.Hour, false},
		{"exactly at threshold", observedAt.Add(time.Hour), time.Hour, false},
		{"just beyond threshold", observedAt.Add(time.Hour + time.Nanosecond), time.Hour, true},
		{"far beyond threshold", observedAt.Add(30 * 24 * time.Hour), time.Hour, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stats.IsStale(tt.now, tt.maxAge); got != tt.want {
				t.Errorf("IsStale() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFingerprintStatsIsStaleNeverObserved(t *testing.T) {
	var stats baseline.FingerprintStats // zero value: Count == 0

	if stats.IsStale(time.Now(), time.Nanosecond) {
		t.Fatalf("IsStale() = true for a never-observed FingerprintStats, want false (cold start, not staleness)")
	}
}
