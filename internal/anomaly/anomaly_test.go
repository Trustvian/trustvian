package anomaly_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/anomaly"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

var testKey = baseline.Key{ActorID: "svc-payment", Environment: "production"}

func stable(target string) features.StableFeatures {
	return features.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryDB,
		OperationName:     "SELECT accounts",
		TargetName:        target,
		Environment:       "production",
	}
}

// matureBaselineInterval is the fixed inter-observation spacing
// matureBaseline uses. Callers that build a Score-time Features against a
// matureBaseline fingerprint and want to observe that fingerprint's
// non-frequency signals in isolation should set
// Volatile.Timestamp to the baseline's LastObserved plus this interval, so
// the new frequency_deviation signal (task 004) doesn't fire unexpectedly
// alongside whatever signal the test actually targets.
const matureBaselineInterval = time.Second

// matureBaseline returns a Baseline where fp has been observed `count`
// times, exactly matureBaselineInterval apart, each with latencyMS latency
// and no errors. A fixed clock (rather than time.Now()) keeps the interval
// EWMA deterministic, matching baselineWithStableInterval's approach.
func matureBaseline(fp fingerprint.Fingerprint, count int, latencyMS float64) baseline.Baseline {
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range count {
		b = b.Observe(fp, features.VolatileFeatures{
			HasLatency: true,
			Latency:    time.Duration(latencyMS * float64(time.Millisecond)),
		}, now)
		now = now.Add(matureBaselineInterval)
	}
	return b
}

func TestScoreNovelDestinationIsAnomalous(t *testing.T) {
	knownFP := fingerprint.Compute(stable("payment-db"))
	novelFeat := features.Features{Stable: stable("admin-db")}
	novelFP := fingerprint.Compute(novelFeat.Stable)

	b := matureBaseline(knownFP, 50, 10)
	cfg := anomaly.DefaultConfig()

	got := anomaly.Score(novelFeat, novelFP, b, cfg)

	if got.Score < 0.9 {
		t.Fatalf("Score = %v for a fingerprint never seen for this actor, want >= 0.9", got.Score)
	}
	if got.Confidence != 0 {
		t.Fatalf("Confidence = %v for a never-observed fingerprint, want 0", got.Confidence)
	}
	if !hasSignal(got.Contributors, "categorical_novelty") {
		t.Fatalf("Contributors = %+v, want categorical_novelty", got.Contributors)
	}
}

func TestScoreLatencySpikeIsAnomalousEvenWhenFamiliar(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	b := matureBaseline(fp, 100, 10) // consistently ~10ms

	spike := features.Features{
		Stable: stable("payment-db"),
		Volatile: features.VolatileFeatures{
			HasLatency: true, Latency: 500 * time.Millisecond,
			Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: isolates the latency signal
		},
	}

	got := anomaly.Score(spike, fp, b, anomaly.DefaultConfig())

	if got.Confidence < 0.99 {
		t.Fatalf("Confidence = %v for a fully mature fingerprint, want ~1", got.Confidence)
	}
	if !hasSignal(got.Contributors, "latency_deviation") {
		t.Fatalf("Contributors = %+v, want latency_deviation", got.Contributors)
	}
	if got.Score < 0.5 {
		t.Fatalf("Score = %v for a 50x latency spike on a familiar fingerprint, want a strong signal", got.Score)
	}
}

func TestScoreColdStartCapsConfidenceNotScore(t *testing.T) {
	feat := features.Features{Stable: stable("payment-db")}
	fp := fingerprint.Compute(feat.Stable)
	empty := baseline.New(testKey)

	got := anomaly.Score(feat, fp, empty, anomaly.DefaultConfig())

	if got.Confidence != 0 {
		t.Fatalf("Confidence = %v on an empty baseline, want 0 (cold start)", got.Confidence)
	}
	if got.Score < 0.9 {
		t.Fatalf("Score = %v on an empty baseline, want a high novelty score despite low confidence", got.Score)
	}
	// The point of this test: Score alone is NOT a safe basis for a
	// BLOCK decision here. A downstream stage must consult Confidence.
}

func TestScoreSensitiveTargetFloorPersistsDespiteFamiliarity(t *testing.T) {
	sensitiveStable := stable("secrets-manager")
	fp := fingerprint.Compute(sensitiveStable)
	b := matureBaseline(fp, 1000, 5) // extremely familiar, rock-steady latency

	cfg := anomaly.DefaultConfig()
	cfg.SensitiveTargetFloor = map[string]float64{"secrets-manager": 0.7}

	feat := features.Features{
		Stable: sensitiveStable,
		Volatile: features.VolatileFeatures{
			HasLatency: true, Latency: 5 * time.Millisecond,
			Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: isolates the sensitive-target floor
		},
	}

	got := anomaly.Score(feat, fp, b, cfg)

	if got.Confidence < 0.99 {
		t.Fatalf("Confidence = %v, want ~1 (this scenario is deliberately maximally familiar)", got.Confidence)
	}
	if got.Score < 0.7 {
		t.Fatalf("Score = %v for a sensitive target, want >= floor 0.7 regardless of familiarity", got.Score)
	}
	if !hasSignal(got.Contributors, "sensitive_target") {
		t.Fatalf("Contributors = %+v, want sensitive_target", got.Contributors)
	}
}

func TestScoreFamiliarConsistentBehaviorIsLowAnomaly(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	b := matureBaseline(fp, 100, 10)

	feat := features.Features{
		Stable: stable("payment-db"),
		Volatile: features.VolatileFeatures{
			HasLatency: true, Latency: 10 * time.Millisecond,
			Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: no frequency_deviation
		},
	}

	got := anomaly.Score(feat, fp, b, anomaly.DefaultConfig())

	if got.Score > 0.1 {
		t.Fatalf("Score = %v for behavior matching a mature, consistent baseline, want ~0", got.Score)
	}
	if got.Confidence < 0.99 {
		t.Fatalf("Confidence = %v, want ~1", got.Confidence)
	}
}

func TestScoreMaturityRampsPartially(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	cfg := anomaly.DefaultConfig() // MinObservations: 20
	b := matureBaseline(fp, 10, 10)

	got := anomaly.Score(features.Features{Stable: stable("payment-db")}, fp, b, cfg)

	if got.Confidence != 0.5 {
		t.Fatalf("Confidence = %v for 10/20 observations, want exactly 0.5", got.Confidence)
	}
}

// baselineWithStableInterval returns a Baseline where fp has been
// observed `count` times, exactly `interval` apart, and no other
// volatile signal set.
func baselineWithStableInterval(fp fingerprint.Fingerprint, interval time.Duration, count int) baseline.Baseline {
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range count {
		b = b.Observe(fp, features.VolatileFeatures{}, now)
		now = now.Add(interval)
	}
	return b
}

// jitterPatternMS is a fixed, deliberately unremarkable sequence of
// per-interval offsets in milliseconds, applied around the nominal
// interval by baselineWithJitteredInterval. It is a literal rather than
// math/rand output so every jitter-based test is exactly reproducible —
// these tests assert on z-scores, which are meaningless if the baseline's
// stddev changes from run to run.
var jitterPatternMS = []int{3, -2, 1, -3, 2, 0, -1, 3, -3, 1, 2, -2, 0, -1, 1}

// baselineWithJitteredInterval is baselineWithStableInterval with ~±3ms
// of jitter on a nominal interval — i.e. what real traffic looks like,
// where no two inter-request gaps are byte-identical. This is what drives
// frequencySignal's *z-score* branch; baselineWithStableInterval's
// perfectly flat cadence always lands in the nearZeroStdDev branch
// instead, which is a different code path entirely.
func baselineWithJitteredInterval(fp fingerprint.Fingerprint, nominal time.Duration, count int) baseline.Baseline {
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range count {
		b = b.Observe(fp, features.VolatileFeatures{}, now)
		now = now.Add(nominal + time.Duration(jitterPatternMS[i%len(jitterPatternMS)])*time.Millisecond)
	}
	return b
}

// TestScoreFrequencyDeviationZScorePath covers frequencySignal's z-score
// branch, which every other frequency test misses: they all build their
// baseline from a perfectly constant interval, so IntervalVariance stays
// exactly 0 and the nearZeroStdDev branch runs instead.
func TestScoreFrequencyDeviationZScorePath(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	bl := baselineWithJitteredInterval(fp, 10*time.Second, 50)
	last := bl.Fingerprints[fp.ID].LastObserved

	// Guard the guard: if this baseline ever stopped producing a
	// meaningful stddev, every assertion below would silently revert to
	// testing the nearZeroStdDev branch again.
	if stddev := math.Sqrt(bl.Fingerprints[fp.ID].IntervalVariance); stddev < float64(time.Millisecond) {
		t.Fatalf("baseline interval stddev = %s, want > 1ms so frequencySignal takes its z-score branch", time.Duration(stddev))
	}

	t.Run("ordinary jitter does not contribute to Score under DefaultConfig", func(t *testing.T) {
		// 5ms off a 10s mean whose own jitter is ~±3ms: a completely
		// ordinary, on-cadence event. Under the pre-fix default
		// (FrequencyWeight 0.6) this scored ~0.47 — enough on its own
		// to push a fully-familiar actor to RiskMedium, which
		// docs/policy-guide.md documents as a typical ALERT/CHALLENGE
		// trigger. The signal now ships inert by default.
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: last.Add(10*time.Second + 5*time.Millisecond)},
		}
		an := anomaly.Score(feat, fp, bl, anomaly.DefaultConfig())

		var found bool
		for _, s := range an.Contributors {
			if s.Name != "frequency_deviation" {
				continue
			}
			found = true
			if !strings.Contains(s.Detail, "z-score") {
				t.Errorf("frequency_deviation Detail = %q, want the z-score branch's wording — this test is not exercising the intended path", s.Detail)
			}
			if contribution := s.Value * s.Weight; contribution != 0 {
				t.Errorf("frequency_deviation contributed %v to the noisy-OR under DefaultConfig, want 0 (the signal is opt-in)", contribution)
			}
		}
		// The signal is still *reported* at weight 0 — Value is
		// computed independently of Weight, and examples/frequency-abuse
		// asserts on Contributors by name.
		if !found {
			t.Error("frequency_deviation absent from Contributors; a zero Weight must silence the score, not the explanation")
		}
		if an.Score != 0 {
			t.Errorf("Score = %v for a mature fingerprint with only ordinary cadence jitter, want exactly 0", an.Score)
		}
	})

	t.Run("genuine spike still fires strongly once an operator opts in", func(t *testing.T) {
		cfg := anomaly.DefaultConfig()
		cfg.FrequencyWeight = 0.6 // what an operator sets after calibrating against their own traffic

		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: last.Add(100 * time.Millisecond)}, // 100x faster than the learned cadence
		}
		an := anomaly.Score(feat, fp, bl, cfg)

		var found bool
		for _, s := range an.Contributors {
			if s.Name != "frequency_deviation" {
				continue
			}
			found = true
			if !strings.Contains(s.Detail, "z-score") {
				t.Errorf("frequency_deviation Detail = %q, want the z-score branch's wording", s.Detail)
			}
			if s.Value < 0.9 {
				t.Errorf("frequency_deviation.Value = %v, want near 1 for a 100x rate spike through the z-score path", s.Value)
			}
		}
		if !found {
			t.Fatal("frequency_deviation did not fire on a 100x rate spike against a jittered baseline")
		}
		if an.Score < 0.5 {
			t.Errorf("Score = %v for a 100x rate spike with FrequencyWeight 0.6, want a strong signal", an.Score)
		}
	})
}

// TestDefaultConfigFrequencyWeightIsOptIn pins the shipped posture
// itself, so re-enabling the signal by default can never be a silent
// one-character change: it has to come with a test update and the
// calibration evidence that justifies it.
func TestDefaultConfigFrequencyWeightIsOptIn(t *testing.T) {
	if w := anomaly.DefaultConfig().FrequencyWeight; w != 0 {
		t.Fatalf("DefaultConfig().FrequencyWeight = %v, want 0 — frequency_deviation ships inert pending real-traffic calibration, mirroring SensitiveTargetFloor's empty default", w)
	}
}

func TestScoreFrequencyDeviation(t *testing.T) {
	cfg := anomaly.DefaultConfig()
	fp := fingerprint.Compute(stable("payment-db"))

	t.Run("normal rate does not fire", func(t *testing.T) {
		bl := baselineWithStableInterval(fp, 10*time.Second, 50)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: bl.Fingerprints[fp.ID].LastObserved.Add(10 * time.Second)},
		}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired on a normal-rate event: %+v", s)
			}
		}
	})

	t.Run("spike fires strongly", func(t *testing.T) {
		bl := baselineWithStableInterval(fp, 10*time.Second, 50)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: bl.Fingerprints[fp.ID].LastObserved.Add(100 * time.Millisecond)},
		}
		an := anomaly.Score(feat, fp, bl, cfg)
		found := false
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				found = true
				if s.Value < 0.9 {
					t.Errorf("frequency_deviation.Value = %v, want near 1 for a 100x rate spike", s.Value)
				}
			}
		}
		if !found {
			t.Error("frequency_deviation did not fire on a 100x rate spike")
		}
	})

	t.Run("cold start does not fire", func(t *testing.T) {
		bl := baseline.New(testKey)
		feat := features.Features{Stable: stable("payment-db"), Volatile: features.VolatileFeatures{Timestamp: time.Now()}}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired on an unknown fingerprint: %+v", s)
			}
		}
	})

	t.Run("negative interval does not fire (out-of-order event)", func(t *testing.T) {
		// An event whose Timestamp precedes the fingerprint's
		// LastObserved (clock skew, out-of-order delivery, a backdated
		// event from an untrusted source) carries no interval
		// information. Treating its negative interval as a measurement
		// would report a maximal deviation for what is really an
		// absence of data — the same reason IntervalObservations == 0
		// is already gated above.
		bl := baselineWithStableInterval(fp, 10*time.Second, 50)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: bl.Fingerprints[fp.ID].LastObserved.Add(-5 * time.Second)},
		}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired on a negative (out-of-order) interval: %+v", s)
			}
		}
	})

	t.Run("negative interval does not fire against a jittered baseline", func(t *testing.T) {
		// Same guard, but through frequencySignal's z-score branch
		// rather than its nearZeroStdDev branch.
		bl := baselineWithJitteredInterval(fp, 10*time.Second, 50)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: bl.Fingerprints[fp.ID].LastObserved.Add(-5 * time.Second)},
		}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired on a negative (out-of-order) interval: %+v", s)
			}
		}
	})

	t.Run("single observation does not fire (no interval yet)", func(t *testing.T) {
		bl := baselineWithStableInterval(fp, 10*time.Second, 1)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: bl.Fingerprints[fp.ID].LastObserved.Add(100 * time.Millisecond)},
		}
		an := anomaly.Score(feat, fp, bl, cfg)
		for _, s := range an.Contributors {
			if s.Name == "frequency_deviation" {
				t.Errorf("frequency_deviation fired after a single observation (no interval baseline yet): %+v", s)
			}
		}
	})
}

func TestScoreErrorAgainstCleanBaselineIsAnomalous(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{Error: false}, now)
		now = now.Add(matureBaselineInterval)
	}

	feat := features.Features{
		Stable: stable("payment-db"),
		Volatile: features.VolatileFeatures{
			Error:     true,
			Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: isolates the error signal
		},
	}

	got := anomaly.Score(feat, fp, b, anomaly.DefaultConfig())

	if !hasSignal(got.Contributors, "error_deviation") {
		t.Fatalf("Contributors = %+v, want error_deviation", got.Contributors)
	}
}

func TestScoreErrorAgainstErrorProneBaselineIsNotAnomalous(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{Error: true}, now)
		now = now.Add(matureBaselineInterval)
	}

	feat := features.Features{
		Stable: stable("payment-db"),
		Volatile: features.VolatileFeatures{
			Error:     true,
			Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: isolates the error signal
		},
	}

	got := anomaly.Score(feat, fp, b, anomaly.DefaultConfig())

	if hasSignal(got.Contributors, "error_deviation") {
		t.Fatalf("Contributors = %+v, error_deviation should not fire when errors are the norm", got.Contributors)
	}
}

func TestScoreMatchesDocumentedNoisyOrFormula(t *testing.T) {
	// Construct a scenario with exactly two known contributing signals
	// (categorical_novelty at partial maturity, and a sensitive-target
	// floor) and verify Score equals 1 - Π(1 - value_i*weight_i) exactly.
	fp := fingerprint.Compute(stable("secrets-manager"))
	cfg := anomaly.DefaultConfig()
	cfg.SensitiveTargetFloor = map[string]float64{"secrets-manager": 0.5}

	b := baselineWithStableInterval(fp, matureBaselineInterval, 10) // 10/20 => familiarity 0.5 => novelty value 0.5

	feat := features.Features{
		Stable: stable("secrets-manager"),
		Volatile: features.VolatileFeatures{
			Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(matureBaselineInterval), // normal rate: isolates novelty + sensitive-target floor
		},
	}
	got := anomaly.Score(feat, fp, b, cfg)

	noveltyContribution := 0.5 * cfg.NoveltyWeight
	floorContribution := 0.5 * 1.0
	want := 1 - (1-noveltyContribution)*(1-floorContribution)

	if diff := got.Score - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula)", got.Score, want)
	}
}

func TestScoreMatchesDocumentedNoisyOrFormulaWithFrequencySignal(t *testing.T) {
	// Same bar as TestScoreMatchesDocumentedNoisyOrFormula, but with
	// categorical_novelty (partial maturity) and frequency_deviation (a
	// rate spike against a rock-steady 10s baseline interval, which
	// takes the nearZeroStdDev branch of frequencySignal and so
	// contributes exactly its full weight) as the two known signals.
	fp := fingerprint.Compute(stable("payment-db"))
	cfg := anomaly.DefaultConfig()
	// FrequencyWeight is 0 by default (the signal is opt-in); set it
	// explicitly, or this test would only prove that multiplying by zero
	// yields zero.
	cfg.FrequencyWeight = 0.6

	b := baselineWithStableInterval(fp, 10*time.Second, 10) // 10/20 => familiarity 0.5 => novelty value 0.5; identical intervals => stddev ~0
	feat := features.Features{
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{Timestamp: b.Fingerprints[fp.ID].LastObserved.Add(100 * time.Millisecond)},
	}
	got := anomaly.Score(feat, fp, b, cfg)

	if !hasSignal(got.Contributors, "frequency_deviation") {
		t.Fatalf("Contributors = %+v, want frequency_deviation", got.Contributors)
	}

	noveltyContribution := 0.5 * cfg.NoveltyWeight
	frequencyContribution := 1.0 * cfg.FrequencyWeight // nearZeroStdDev branch: value is exactly 1
	want := 1 - (1-noveltyContribution)*(1-frequencyContribution)

	if diff := got.Score - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula, including frequency_deviation)", got.Score, want)
	}
}

func TestScoreMatchesDocumentedNoisyOrFormulaWithTimePatternSignal(t *testing.T) {
	// Same bar as the frequency-signal sibling test above: reproduce
	// combine()'s exact noisy-OR arithmetic with a firing
	// time_pattern_deviation signal included, not just spot-check its
	// direction.
	fp := fingerprint.Compute(stable("payment-db"))
	cfg := anomaly.DefaultConfig()
	// TimePatternWeight is 0 by default (opt-in); set it explicitly.
	cfg.TimePatternWeight = 0.6

	const observedHour = 9
	b := baselineAtHour(fp, observedHour, int(cfg.MinObservations)+10)
	// A different hour: activity there is exactly 0 (one-hot seeded,
	// never updated toward 1 since every observation was at
	// observedHour), so timePatternSignal's value is exactly 1 —
	// the same nearZeroStdDev-style "fully deterministic" case
	// frequencySignal's own exact-formula sibling test relies on.
	const novelHour = 3
	feat := features.Features{
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{Timestamp: time.Date(2026, 6, 1, novelHour, 0, 0, 0, time.UTC)},
	}
	got := anomaly.Score(feat, fp, b, cfg)

	if !hasSignal(got.Contributors, "time_pattern_deviation") {
		t.Fatalf("Contributors = %+v, want time_pattern_deviation", got.Contributors)
	}

	// Fingerprint is fully mature (Count > MinObservations), so
	// categorical_novelty does not fire at all.
	timePatternContribution := 1.0 * cfg.TimePatternWeight // HourActivity[novelHour] == 0 exactly
	want := 1 - (1 - timePatternContribution)

	if diff := got.Score - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula, including time_pattern_deviation)", got.Score, want)
	}
}

func TestScoreFingerprintIDMatchesComputedFingerprint(t *testing.T) {
	feat := features.Features{Stable: stable("payment-db")}
	fp := fingerprint.Compute(feat.Stable)
	want := fp.ID

	got := anomaly.Score(feat, fp, baseline.New(testKey), anomaly.DefaultConfig())

	if got.FingerprintID != want {
		t.Fatalf("FingerprintID = %q, want %q", got.FingerprintID, want)
	}
}

// baselineAtHour returns a Baseline where fp has been observed count
// times, always at the same UTC hour-of-day (different calendar days),
// mirroring internal/baseline's own test helper of the same shape.
func baselineAtHour(fp fingerprint.Fingerprint, hour, count int) baseline.Baseline {
	b := baseline.New(testKey)
	day := time.Date(2026, 1, 1, hour, 0, 0, 0, time.UTC)
	for i := range count {
		b = b.Observe(fp, features.VolatileFeatures{}, day.AddDate(0, 0, i))
	}
	return b
}

// baselineUniformAcrossHours returns a Baseline where fp has been
// observed once per hour, every hour, for the given number of days —
// the "no real time-of-day pattern" fixture.
func baselineUniformAcrossHours(fp fingerprint.Fingerprint, days int) baseline.Baseline {
	b := baseline.New(testKey)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for day := range days {
		for hour := range 24 {
			b = b.Observe(fp, features.VolatileFeatures{}, start.AddDate(0, 0, day).Add(time.Duration(hour)*time.Hour))
		}
	}
	return b
}

func TestScoreTimePatternDeviation(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	cfg := anomaly.DefaultConfig()
	cfg.TimePatternWeight = 0.6 // opt-in: default is 0, see TestDefaultConfigTimePatternWeightIsOptIn

	t.Run("normal hour does not fire", func(t *testing.T) {
		b := baselineAtHour(fp, 9, int(cfg.MinObservations)+10)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)},
		}
		got := anomaly.Score(feat, fp, b, cfg)
		if hasSignal(got.Contributors, "time_pattern_deviation") {
			t.Errorf("time_pattern_deviation fired for an event at this fingerprint's established hour: %+v", got.Contributors)
		}
	})

	t.Run("novel hour fires strongly", func(t *testing.T) {
		b := baselineAtHour(fp, 9, int(cfg.MinObservations)+10)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)},
		}
		got := anomaly.Score(feat, fp, b, cfg)
		found := false
		for _, s := range got.Contributors {
			if s.Name == "time_pattern_deviation" {
				found = true
				if s.Value < 0.9 {
					t.Errorf("time_pattern_deviation.Value = %v, want near 1 for an hour this fingerprint has never fired at", s.Value)
				}
			}
		}
		if !found {
			t.Error("time_pattern_deviation did not fire for an hour this fingerprint has never been observed at")
		}
	})

	t.Run("cold start does not fire", func(t *testing.T) {
		b := baseline.New(testKey)
		feat := features.Features{Stable: stable("payment-db"), Volatile: features.VolatileFeatures{Timestamp: time.Now()}}
		got := anomaly.Score(feat, fp, b, cfg)
		if hasSignal(got.Contributors, "time_pattern_deviation") {
			t.Errorf("time_pattern_deviation fired on an unknown fingerprint: %+v", got.Contributors)
		}
	})

	t.Run("below MinObservations does not fire even at a novel hour", func(t *testing.T) {
		b := baselineAtHour(fp, 9, int(cfg.MinObservations)-1)
		feat := features.Features{
			Stable:   stable("payment-db"),
			Volatile: features.VolatileFeatures{Timestamp: time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)},
		}
		got := anomaly.Score(feat, fp, b, cfg)
		if hasSignal(got.Contributors, "time_pattern_deviation") {
			t.Errorf("time_pattern_deviation fired before TimePatternObservations reached MinObservations: %+v", got.Contributors)
		}
	})

	// Uniform hour-of-day traffic produces a small, bounded residual
	// reading, not an exact zero: HourActivity is only updated once
	// per 24 observations for any given bucket (see
	// hourActivityAlpha's doc comment in internal/baseline/baseline.go),
	// so at any single instant the 24 buckets sit at different points
	// along their own decay-then-refresh cycle relative to each other,
	// even though the underlying traffic has no real pattern. This is
	// the documented, accepted tradeoff of a bounded-memory EWMA — the
	// bound matters, not an unachievable exact zero, and it is exactly
	// why TimePatternWeight ships at 0 by default (see
	// TestDefaultConfigTimePatternWeightIsOptIn): an operator who
	// enables the signal is opting into this bounded noise floor, not
	// a false claim that it doesn't exist.
	t.Run("uniform traffic across all hours stays within the documented noise bound", func(t *testing.T) {
		b := baselineUniformAcrossHours(fp, 30)
		const maxUniformNoise = 0.3 // measured worst case ~0.22 at hourActivityAlpha=0.02; 0.3 leaves headroom without hiding a regression
		for hour := range 24 {
			feat := features.Features{
				Stable:   stable("payment-db"),
				Volatile: features.VolatileFeatures{Timestamp: time.Date(2026, 6, 1, hour, 0, 0, 0, time.UTC)},
			}
			got := anomaly.Score(feat, fp, b, cfg)
			for _, s := range got.Contributors {
				if s.Name == "time_pattern_deviation" && s.Value > maxUniformNoise {
					t.Errorf("hour %d: time_pattern_deviation.Value = %v, want <= %v for genuinely uniform hour-of-day traffic", hour, s.Value, maxUniformNoise)
				}
			}
		}
	})
}

// TestScoreTimePatternIgnoresZeroValueHourActivity is the persistence-
// migration regression test task 017 requires: a FingerprintStats with
// a mature Count but a zero-value HourActivity (simulating one loaded
// from a store.FileStore file written before this task) must not fire
// the signal — TimePatternObservations, not Count, is what gates it.
func TestScoreTimePatternIgnoresZeroValueHourActivity(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	cfg := anomaly.DefaultConfig()
	cfg.TimePatternWeight = 0.6

	// Build a mature baseline the normal way, then simulate a
	// pre-task-017 persisted record by resetting only the new fields —
	// Count (and everything else) stays mature.
	b := matureBaseline(fp, int(cfg.MinObservations)+10, 10)
	stats := b.Fingerprints[fp.ID]
	stats.HourActivity = [24]float64{}
	stats.TimePatternObservations = 0
	b.Fingerprints[fp.ID] = stats

	feat := features.Features{
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{Timestamp: time.Date(2026, 6, 1, 3, 0, 0, 0, time.UTC)},
	}
	got := anomaly.Score(feat, fp, b, cfg)
	if hasSignal(got.Contributors, "time_pattern_deviation") {
		t.Errorf("time_pattern_deviation fired for a migrated record with zero-value HourActivity: %+v", got.Contributors)
	}
}

func TestDefaultConfigTimePatternWeightIsOptIn(t *testing.T) {
	cfg := anomaly.DefaultConfig()
	if cfg.TimePatternWeight != 0 {
		t.Errorf("DefaultConfig().TimePatternWeight = %v, want 0 (opt-in, like FrequencyWeight)", cfg.TimePatternWeight)
	}
}

func TestDefaultConfigTransitionWeightIsOptIn(t *testing.T) {
	cfg := anomaly.DefaultConfig()
	if cfg.TransitionWeight != 0 {
		t.Errorf("DefaultConfig().TransitionWeight = %v, want 0 (opt-in, like FrequencyWeight/TimePatternWeight)", cfg.TransitionWeight)
	}
}

// TestScoreTransitionDeviation is the v0.6 foundation's own signal
// test, mirroring TestScoreFrequencyDeviation/TestScoreTimePatternDeviation's
// table shape.
func TestScoreTransitionDeviation(t *testing.T) {
	fpRead := fingerprint.Compute(stable("customer-db"))
	fpUpdate := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "UPDATE accounts", TargetName: "customer-db", Environment: "production",
	})
	fpDelete := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "DELETE accounts", TargetName: "customer-db", Environment: "production",
	})

	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7 // opt-in: default is 0, see TestDefaultConfigTransitionWeightIsOptIn

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("familiar transition does not fire", func(t *testing.T) {
		// read -> update, observed many times.
		b := baseline.New(testKey)
		now := base
		for range 20 {
			b = b.Observe(fpRead, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
			b = b.Observe(fpUpdate, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
		}
		// One more read, then score the next update as the event under test.
		b = b.Observe(fpRead, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)

		feat := features.Features{Stable: fpUpdate.Stable, Volatile: features.VolatileFeatures{Timestamp: now}}
		got := anomaly.Score(feat, fpUpdate, b, cfg)

		if hasSignal(got.Contributors, "transition_deviation") {
			t.Fatalf("Contributors = %+v, want no transition_deviation for a transition observed 20+ times", got.Contributors)
		}
	})

	t.Run("unseen transition fires strongly", func(t *testing.T) {
		// The actor's history only ever shows read -> update; a
		// read -> delete transition has never been observed.
		b := baseline.New(testKey)
		now := base
		for range 20 {
			b = b.Observe(fpRead, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
			b = b.Observe(fpUpdate, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
		}
		b = b.Observe(fpRead, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)

		feat := features.Features{Stable: fpDelete.Stable, Volatile: features.VolatileFeatures{Timestamp: now}}
		got := anomaly.Score(feat, fpDelete, b, cfg)

		if !hasSignal(got.Contributors, "transition_deviation") {
			t.Fatalf("Contributors = %+v, want transition_deviation for a never-observed read->delete transition", got.Contributors)
		}
	})

	t.Run("first-ever event has no predecessor, does not fire", func(t *testing.T) {
		b := baseline.New(testKey)
		feat := features.Features{Stable: fpRead.Stable, Volatile: features.VolatileFeatures{Timestamp: base}}
		got := anomaly.Score(feat, fpRead, b, cfg)

		if hasSignal(got.Contributors, "transition_deviation") {
			t.Fatalf("Contributors = %+v, want no transition_deviation on an actor's first-ever event (no predecessor)", got.Contributors)
		}
	})

	t.Run("out-of-order event does not fire", func(t *testing.T) {
		b := baseline.New(testKey)
		b = b.Observe(fpRead, features.VolatileFeatures{}, base.Add(2*time.Second))

		// A backdated event, timestamped before fpRead's own arrival,
		// does not validly follow it — see baseline.Baseline.Observe's
		// identical guard.
		feat := features.Features{Stable: fpDelete.Stable, Volatile: features.VolatileFeatures{Timestamp: base.Add(time.Second)}}
		got := anomaly.Score(feat, fpDelete, b, cfg)

		if hasSignal(got.Contributors, "transition_deviation") {
			t.Fatalf("Contributors = %+v, want no transition_deviation for an out-of-order event", got.Contributors)
		}
	})
}

// TestScoreMatchesDocumentedNoisyOrFormulaWithTransitionSignal mirrors
// TestScoreMatchesDocumentedNoisyOrFormulaWithTimePatternSignal's exact
// bar: reproduce combine()'s noisy-OR arithmetic with a firing
// transition_deviation signal, not just spot-check its direction.
func TestScoreMatchesDocumentedNoisyOrFormulaWithTransitionSignal(t *testing.T) {
	fpRead := fingerprint.Compute(stable("customer-db"))
	fpDelete := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "DELETE accounts", TargetName: "customer-db", Environment: "production",
	})

	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7

	// A mature fpDelete (so categorical_novelty does not also fire),
	// but with a predecessor (fpRead) it has never transitioned from.
	b := matureBaseline(fpDelete, int(cfg.MinObservations)+10, 10)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Overwrite the predecessor with fpRead just before the event under
	// test, at a time strictly after the baseline's own last write.
	last := now.Add(time.Duration(int(cfg.MinObservations)+10) * matureBaselineInterval)
	b = b.Observe(fpRead, features.VolatileFeatures{}, last)

	eventTime := last.Add(matureBaselineInterval)
	feat := features.Features{Stable: fpDelete.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
	got := anomaly.Score(feat, fpDelete, b, cfg)

	if !hasSignal(got.Contributors, "transition_deviation") {
		t.Fatalf("Contributors = %+v, want transition_deviation", got.Contributors)
	}

	transitionContribution := 1.0 * cfg.TransitionWeight // never-seen transition -> Value 1
	want := 1 - (1 - transitionContribution)

	if diff := got.Score - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula, including transition_deviation)", got.Score, want)
	}
}

// transitionRarityBaseline builds a Baseline where predecessor
// precedes each fingerprint in dests[i] exactly counts[i] times,
// interleaved as predecessor -> dests[i] -> predecessor -> dests[i] ->
// ... for each i in turn, so predecessor's own OutgoingTransitionTotal
// and each dests[i]'s PredecessorCounts[predecessor.ID] end up with
// exactly the requested counts, with no cross-contamination between
// them. Ends with one final, unpaired observation of predecessor
// itself, so the returned Baseline's LastFingerprintID is predecessor
// — the caller can then score any dests[i] as the event under test and
// have it correctly evaluated as a predecessor -> dests[i] transition,
// not a dests[i] -> dests[i] self-transition (which is what the
// baseline's LastFingerprintID would otherwise be, since the loop
// above always observes a dest last).
func transitionRarityBaseline(predecessor fingerprint.Fingerprint, dests []fingerprint.Fingerprint, counts []int) baseline.Baseline {
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, dest := range dests {
		for range counts[i] {
			b = b.Observe(predecessor, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
			b = b.Observe(dest, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
		}
	}
	b = b.Observe(predecessor, features.VolatileFeatures{}, now)
	return b
}

func TestDefaultConfigTransitionRarityWeightIsOptIn(t *testing.T) {
	cfg := anomaly.DefaultConfig()
	if cfg.TransitionRarityWeight != 0 {
		t.Errorf("DefaultConfig().TransitionRarityWeight = %v, want 0 (opt-in, like every other v0.6 signal weight)", cfg.TransitionRarityWeight)
	}
	if cfg.MinTransitionObservations != 20 {
		t.Errorf("DefaultConfig().MinTransitionObservations = %v, want 20 (matching MinObservations's own default)", cfg.MinTransitionObservations)
	}
}

// TestScoreTransitionRarityOrdering is this task's own central
// statistical-correctness test: common, uncommon, and rare transitions
// from the same predecessor must produce strictly increasing rarity
// values, and an unseen transition must not be scored by this signal
// at all (transition_deviation, not transition_rarity, covers it).
func TestScoreTransitionRarityOrdering(t *testing.T) {
	predecessor := fingerprint.Compute(stable("read"))
	common := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "common", TargetName: "customer-db", Environment: "production",
	})
	uncommon := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "uncommon", TargetName: "customer-db", Environment: "production",
	})
	rare := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "rare", TargetName: "customer-db", Environment: "production",
	})
	unseen := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "unseen", TargetName: "customer-db", Environment: "production",
	})

	// 900 + 90 + 10 = 1000 total outgoing transitions from predecessor.
	b := transitionRarityBaseline(predecessor, []fingerprint.Fingerprint{common, uncommon, rare}, []int{900, 90, 10})

	cfg := anomaly.DefaultConfig()
	cfg.TransitionRarityWeight = 0.8
	cfg.TransitionWeight = 0.8

	scoreFor := func(dest fingerprint.Fingerprint) anomaly.Anomaly {
		eventTime := b.LastFingerprintTime.Add(time.Second)
		feat := features.Features{Stable: dest.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
		return anomaly.Score(feat, dest, b, cfg)
	}

	rarityValue := func(a anomaly.Anomaly) float64 {
		for _, c := range a.Contributors {
			if c.Name == "transition_rarity" {
				return c.Value
			}
		}
		return -1 // sentinel: not found
	}

	commonAnomaly := scoreFor(common)
	uncommonAnomaly := scoreFor(uncommon)
	rareAnomaly := scoreFor(rare)
	unseenAnomaly := scoreFor(unseen)

	commonRarity, uncommonRarity, rareRarity := rarityValue(commonAnomaly), rarityValue(uncommonAnomaly), rarityValue(rareAnomaly)

	if commonRarity < 0 || uncommonRarity < 0 || rareRarity < 0 {
		t.Fatalf("expected transition_rarity on all three seen transitions: common=%v uncommon=%v rare=%v", commonRarity, uncommonRarity, rareRarity)
	}
	if !(commonRarity < uncommonRarity && uncommonRarity < rareRarity) {
		t.Fatalf("rarity ordering violated: common=%v, uncommon=%v, rare=%v — want common < uncommon < rare", commonRarity, uncommonRarity, rareRarity)
	}

	// The unseen transition must carry transition_deviation, not
	// transition_rarity — the two are mutually exclusive.
	if hasSignal(unseenAnomaly.Contributors, "transition_rarity") {
		t.Error("unseen transition carried transition_rarity, want it to carry only transition_deviation")
	}
	if !hasSignal(unseenAnomaly.Contributors, "transition_deviation") {
		t.Error("unseen transition did not carry transition_deviation")
	}
	if hasSignal(commonAnomaly.Contributors, "transition_deviation") {
		t.Error("common (seen) transition carried transition_deviation, want only transition_rarity")
	}
}

// TestScoreTransitionRarityColdStart proves the minimum-support gate:
// below MinTransitionObservations, transition_rarity does not fire at
// all, regardless of how the observed transitions are distributed.
func TestScoreTransitionRarityColdStart(t *testing.T) {
	predecessor := fingerprint.Compute(stable("read"))
	destA := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "a", TargetName: "customer-db", Environment: "production",
	})
	destB := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "b", TargetName: "customer-db", Environment: "production",
	})

	cfg := anomaly.DefaultConfig()
	cfg.TransitionRarityWeight = 0.8
	const minSupport = 20
	cfg.MinTransitionObservations = minSupport

	tests := []struct {
		name          string
		totalOutgoing int
		wantFire      bool
	}{
		{"zero observations", 0, false},
		{"one observation", 1, false},
		{"minSupport - 1", minSupport - 1, false},
		{"minSupport exactly", minSupport, true},
		{"minSupport + 1", minSupport + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// destA absorbs all-but-one of the outgoing transitions;
			// destB absorbs exactly one, so a nonzero, non-total
			// frequency exists for destB whenever totalOutgoing > 0.
			var b baseline.Baseline
			if tt.totalOutgoing == 0 {
				b = baseline.New(testKey)
			} else if tt.totalOutgoing == 1 {
				b = transitionRarityBaseline(predecessor, []fingerprint.Fingerprint{destB}, []int{1})
			} else {
				b = transitionRarityBaseline(predecessor, []fingerprint.Fingerprint{destA, destB}, []int{tt.totalOutgoing - 1, 1})
			}

			eventTime := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
			if !b.LastFingerprintTime.IsZero() {
				eventTime = b.LastFingerprintTime.Add(time.Second)
			}
			feat := features.Features{Stable: destB.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
			got := anomaly.Score(feat, destB, b, cfg)

			fired := hasSignal(got.Contributors, "transition_rarity")
			if fired != tt.wantFire {
				t.Errorf("transition_rarity fired = %v, want %v (totalOutgoing=%d, minSupport=%d)", fired, tt.wantFire, tt.totalOutgoing, minSupport)
			}
		})
	}
}

// TestScoreMatchesDocumentedFormulaForTransitionRarity reproduces the
// exact frequency/rarity arithmetic documented in
// docs/adr/0011-transition-rarity-statistic-and-orientation.md —
// "the score went up" is not enough for a security-relevant number.
func TestScoreMatchesDocumentedFormulaForTransitionRarity(t *testing.T) {
	predecessor := fingerprint.Compute(stable("read"))
	common := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "common", TargetName: "customer-db", Environment: "production",
	})
	rare := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "rare", TargetName: "customer-db", Environment: "production",
	})

	// 96 + 4 = 100 total; rare's frequency is exactly 4/100 = 0.04.
	b := transitionRarityBaseline(predecessor, []fingerprint.Fingerprint{common, rare}, []int{96, 4})

	cfg := anomaly.DefaultConfig()
	cfg.TransitionRarityWeight = 0.5

	eventTime := b.LastFingerprintTime.Add(time.Second)
	feat := features.Features{Stable: rare.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
	got := anomaly.Score(feat, rare, b, cfg)

	wantFrequency := 4.0 / 100.0
	wantValue := 1 - wantFrequency // 0.96

	var gotValue float64
	found := false
	for _, c := range got.Contributors {
		if c.Name == "transition_rarity" {
			gotValue = c.Value
			found = true
		}
	}
	if !found {
		t.Fatalf("Contributors = %+v, want transition_rarity", got.Contributors)
	}
	if diff := gotValue - wantValue; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("transition_rarity Value = %v, want %v (1 - 4/100)", gotValue, wantValue)
	}

	// And the combined Score, via the documented noisy-OR formula —
	// including categorical_novelty, which also genuinely fires here:
	// rare's own Count is 4 (< MinObservations=20), so it is not yet
	// fully mature as a fingerprint in its own right, independent of
	// how rare the specific read->rare transition is. This mirrors
	// TestScoreMatchesDocumentedNoisyOrFormulaWithFrequencySignal's own
	// approach: account for a genuinely-firing categorical_novelty
	// explicitly, rather than engineering it away.
	familiarity := min(4.0/float64(cfg.MinObservations), 1)
	noveltyContribution := (1 - familiarity) * cfg.NoveltyWeight
	rarityContribution := wantValue * cfg.TransitionRarityWeight
	wantScore := 1 - (1-noveltyContribution)*(1-rarityContribution)
	if diff := got.Score - wantScore; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula)", got.Score, wantScore)
	}
}

// TestScoreTransitionRarityNeverExceedsBounds is the defensive-numeric
// property test section 12 asks for: across a range of counts and
// totals, transition_rarity's Value must always land in [0,1] — never
// negative, never >1, never NaN, never Inf.
func TestScoreTransitionRarityNeverExceedsBounds(t *testing.T) {
	predecessor := fingerprint.Compute(stable("read"))
	cfg := anomaly.DefaultConfig()
	cfg.TransitionRarityWeight = 1.0
	cfg.MinTransitionObservations = 1

	for _, total := range []int{1, 2, 5, 20, 1000} {
		dest := fingerprint.Compute(features.StableFeatures{
			ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
			OperationName: fmt.Sprintf("dest-%d", total), TargetName: "customer-db", Environment: "production",
		})
		b := transitionRarityBaseline(predecessor, []fingerprint.Fingerprint{dest}, []int{total})

		eventTime := b.LastFingerprintTime.Add(time.Second)
		feat := features.Features{Stable: dest.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
		got := anomaly.Score(feat, dest, b, cfg)

		for _, c := range got.Contributors {
			if c.Name != "transition_rarity" {
				continue
			}
			if math.IsNaN(c.Value) || math.IsInf(c.Value, 0) {
				t.Errorf("total=%d: transition_rarity Value = %v, want a finite number", total, c.Value)
			}
			if c.Value < 0 || c.Value > 1 {
				t.Errorf("total=%d: transition_rarity Value = %v, want in [0,1]", total, c.Value)
			}
		}
	}
}

// --- Task 027: bounded 3-gram behavioral detection ---

func TestDefaultConfigNGramWeightIsOptIn(t *testing.T) {
	cfg := anomaly.DefaultConfig()
	if cfg.NGramWeight != 0 {
		t.Errorf("DefaultConfig().NGramWeight = %v, want 0 (opt-in, like every other v0.6 signal weight)", cfg.NGramWeight)
	}
	if cfg.NGramRarityWeight != 0 {
		t.Errorf("DefaultConfig().NGramRarityWeight = %v, want 0", cfg.NGramRarityWeight)
	}
	if cfg.MinNGramObservations != 20 {
		t.Errorf("DefaultConfig().MinNGramObservations = %v, want 20 (matching MinObservations/MinTransitionObservations's own default)", cfg.MinNGramObservations)
	}
}

// ngramBaseline builds a Baseline where the pair (grandparent,
// predecessor) precedes each fingerprint in dests[i] exactly counts[i]
// times, interleaved as
// grandparent -> predecessor -> dests[i] -> grandparent -> predecessor
// -> dests[i] -> ... for each i in turn, so each dests[i]'s
// TrigramCounts[{grandparent,predecessor}] and predecessor's own
// TrigramContinuationTotal[grandparent] end up with exactly the sum of
// the requested counts. Ends with two final, unpaired observations
// (grandparent, then predecessor), so the returned Baseline's
// (PreviousFingerprintID, LastFingerprintID) = (grandparent,
// predecessor) — the caller can then score any dests[i] as the event
// under test and have it correctly evaluated as a
// (grandparent, predecessor) -> dests[i] 3-gram, mirroring
// transitionRarityBaseline's own "one final unpaired observation"
// technique, extended by one more step since a complete 3-gram window
// needs two prior fingerprints positioned correctly, not one.
func ngramBaseline(grandparent, predecessor fingerprint.Fingerprint, dests []fingerprint.Fingerprint, counts []int) baseline.Baseline {
	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, dest := range dests {
		for range counts[i] {
			b = b.Observe(grandparent, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
			b = b.Observe(predecessor, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
			b = b.Observe(dest, features.VolatileFeatures{}, now)
			now = now.Add(time.Second)
		}
	}
	b = b.Observe(grandparent, features.VolatileFeatures{}, now)
	now = now.Add(time.Second)
	b = b.Observe(predecessor, features.VolatileFeatures{}, now)
	return b
}

// TestScoreNoNGramSignalBeforeTrigramHistoryExists is task 027's own
// cold-start proof, following the task brief's own example precisely:
// event #1 and #2 have insufficient history for a 3-gram (no
// ngram_deviation/ngram_rarity at all — not even a "novel" reading,
// since there is no trigram to evaluate yet); event #3 is the first
// with a complete 3-gram.
func TestScoreNoNGramSignalBeforeTrigramHistoryExists(t *testing.T) {
	fpAuth := fingerprint.Compute(stable("auth-service"))
	fpRead := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	fpExport := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "export_customer", TargetName: "customer-db", Environment: "production",
	})

	cfg := anomaly.DefaultConfig()
	cfg.NGramWeight = 0.7
	cfg.NGramRarityWeight = 0.7

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := baseline.New(testKey)

	// Event #1: no prior history at all.
	feat1 := features.Features{Stable: fpAuth.Stable, Volatile: features.VolatileFeatures{Timestamp: base}}
	got1 := anomaly.Score(feat1, fpAuth, b, cfg)
	if hasSignal(got1.Contributors, "ngram_deviation") || hasSignal(got1.Contributors, "ngram_rarity") {
		t.Fatalf("event #1: Contributors = %+v, want no ngram signal (no history)", got1.Contributors)
	}
	b = b.Observe(fpAuth, features.VolatileFeatures{}, base)

	// Event #2: one prior observation — a predecessor exists, but no
	// grandparent yet, so still insufficient for a 3-gram.
	t2 := base.Add(time.Second)
	feat2 := features.Features{Stable: fpRead.Stable, Volatile: features.VolatileFeatures{Timestamp: t2}}
	got2 := anomaly.Score(feat2, fpRead, b, cfg)
	if hasSignal(got2.Contributors, "ngram_deviation") || hasSignal(got2.Contributors, "ngram_rarity") {
		t.Fatalf("event #2: Contributors = %+v, want no ngram signal (only one prior observation)", got2.Contributors)
	}
	b = b.Observe(fpRead, features.VolatileFeatures{}, t2)

	// Event #3: both a predecessor and a grandparent now exist — the
	// first complete 3-gram.
	t3 := t2.Add(time.Second)
	feat3 := features.Features{Stable: fpExport.Stable, Volatile: features.VolatileFeatures{Timestamp: t3}}
	got3 := anomaly.Score(feat3, fpExport, b, cfg)
	if !hasSignal(got3.Contributors, "ngram_deviation") {
		t.Fatalf("event #3: Contributors = %+v, want ngram_deviation (first complete, never-seen 3-gram)", got3.Contributors)
	}
}

func TestScoreNGramDeviationFamiliarTrigram(t *testing.T) {
	fpAuth := fingerprint.Compute(stable("auth-service"))
	fpRead := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	fpExport := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "export_customer", TargetName: "customer-db", Environment: "production",
	})

	// authenticate -> read_customer -> export_customer, trained many times.
	b := ngramBaseline(fpAuth, fpRead, []fingerprint.Fingerprint{fpExport}, []int{25})

	cfg := anomaly.DefaultConfig()
	cfg.NGramWeight = 0.7

	eventTime := b.LastFingerprintTime.Add(time.Second)
	feat := features.Features{Stable: fpExport.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
	got := anomaly.Score(feat, fpExport, b, cfg)

	if hasSignal(got.Contributors, "ngram_deviation") {
		t.Fatalf("Contributors = %+v, want no ngram_deviation for a 3-gram observed 25 times", got.Contributors)
	}
}

// TestScoreNGramDeviationNovelTrigram trains two distinct
// continuations from the same (grandparent, predecessor) pair, then
// evaluates a third, never-trained continuation — ngram_deviation must
// fire.
func TestScoreNGramDeviationNovelTrigram(t *testing.T) {
	fpAuth := fingerprint.Compute(stable("auth-service"))
	fpRead := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	fpUpdate := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "update_customer", TargetName: "customer-db", Environment: "production",
	})
	fpExport := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "export_customer", TargetName: "customer-db", Environment: "production",
	})
	fpDelete := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "delete_customer", TargetName: "customer-db", Environment: "production",
	})

	// authenticate -> read_customer -> {update_customer, export_customer}
	// — never -> delete_customer.
	b := ngramBaseline(fpAuth, fpRead, []fingerprint.Fingerprint{fpUpdate, fpExport}, []int{15, 15})

	cfg := anomaly.DefaultConfig()
	cfg.NGramWeight = 0.7

	eventTime := b.LastFingerprintTime.Add(time.Second)
	feat := features.Features{Stable: fpDelete.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
	got := anomaly.Score(feat, fpDelete, b, cfg)

	if !hasSignal(got.Contributors, "ngram_deviation") {
		t.Fatalf("Contributors = %+v, want ngram_deviation for a never-observed authenticate->read_customer->delete_customer 3-gram", got.Contributors)
	}
}

// TestScoreNGramDeviationDetectsNovelTrigramDespiteFamiliarPairwiseTransitions
// is task 027's mandatory critical-semantic proof (§39): both
// individual pairwise transitions (authenticate->read_customer and
// read_customer->export_customer) are independently familiar — neither
// transition_deviation nor transition_rarity fires for the final hop —
// yet the complete 3-gram authenticate->read_customer->export_customer
// has never actually occurred as a single sequence. ngram_deviation
// must still detect this higher-order novelty. This is the test that
// proves the 3-gram detector adds genuine information beyond what
// tasks 025/026's pairwise signals already provide.
func TestScoreNGramDeviationDetectsNovelTrigramDespiteFamiliarPairwiseTransitions(t *testing.T) {
	fpAuth := fingerprint.Compute(stable("auth-service"))
	fpRead := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	fpExport := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "export_customer", TargetName: "customer-db", Environment: "production",
	})
	// Distinct fingerprints used only to make authenticate->read_customer
	// and read_customer->export_customer each familiar independently,
	// via a *different* 3-gram context each time, so the exact 3-gram
	// (authenticate, read_customer, export_customer) is never trained.
	fpOtherStart := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "other_start", TargetName: "customer-db", Environment: "production",
	})
	fpOtherEnd := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "other_end", TargetName: "customer-db", Environment: "production",
	})

	b := baseline.New(testKey)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Make authenticate -> read_customer familiar, via
	// authenticate -> read_customer -> other_end (never export_customer).
	for range 20 {
		b = b.Observe(fpAuth, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
		b = b.Observe(fpRead, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
		b = b.Observe(fpOtherEnd, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
	}

	// Make read_customer -> export_customer familiar, via
	// other_start -> read_customer -> export_customer (never
	// preceded by authenticate).
	for range 20 {
		b = b.Observe(fpOtherStart, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
		b = b.Observe(fpRead, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
		b = b.Observe(fpExport, features.VolatileFeatures{}, now)
		now = now.Add(time.Second)
	}

	// Position the history window at (authenticate, read_customer) —
	// the real sequence under test — without ever having observed
	// export_customer as its continuation.
	b = b.Observe(fpAuth, features.VolatileFeatures{}, now)
	now = now.Add(time.Second)
	b = b.Observe(fpRead, features.VolatileFeatures{}, now)
	now = now.Add(time.Second)

	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 0.7
	cfg.TransitionRarityWeight = 0.7
	cfg.NGramWeight = 0.7
	cfg.MinTransitionObservations = 1

	feat := features.Features{Stable: fpExport.Stable, Volatile: features.VolatileFeatures{Timestamp: now}}
	got := anomaly.Score(feat, fpExport, b, cfg)

	// The pairwise hop (read_customer -> export_customer) is genuinely
	// familiar: no transition_deviation.
	if hasSignal(got.Contributors, "transition_deviation") {
		t.Fatalf("Contributors = %+v, want no transition_deviation — read_customer->export_customer is a familiar pairwise transition", got.Contributors)
	}
	// The 3-gram (authenticate, read_customer) -> export_customer has
	// never been observed as a complete sequence: ngram_deviation must
	// fire despite both individual hops being familiar.
	if !hasSignal(got.Contributors, "ngram_deviation") {
		t.Fatalf("Contributors = %+v, want ngram_deviation — the complete 3-gram was never observed, even though both pairwise transitions are familiar", got.Contributors)
	}
}

// TestScoreNGramRarityOrdering mirrors TestScoreTransitionRarityOrdering
// one level up: common, uncommon, and rare continuations from the same
// (grandparent, predecessor) pair must produce strictly increasing
// rarity values, and an unseen continuation must carry ngram_deviation,
// not ngram_rarity.
func TestScoreNGramRarityOrdering(t *testing.T) {
	grandparent := fingerprint.Compute(stable("auth-service"))
	predecessor := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	common := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "common", TargetName: "customer-db", Environment: "production",
	})
	uncommon := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "uncommon", TargetName: "customer-db", Environment: "production",
	})
	rare := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "rare", TargetName: "customer-db", Environment: "production",
	})
	unseen := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "unseen", TargetName: "customer-db", Environment: "production",
	})

	// 900 + 90 + 10 = 1000 total continuations from (grandparent, predecessor).
	b := ngramBaseline(grandparent, predecessor, []fingerprint.Fingerprint{common, uncommon, rare}, []int{900, 90, 10})

	cfg := anomaly.DefaultConfig()
	cfg.NGramRarityWeight = 0.8
	cfg.NGramWeight = 0.8

	scoreFor := func(dest fingerprint.Fingerprint) anomaly.Anomaly {
		eventTime := b.LastFingerprintTime.Add(time.Second)
		feat := features.Features{Stable: dest.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
		return anomaly.Score(feat, dest, b, cfg)
	}

	rarityValue := func(a anomaly.Anomaly) float64 {
		for _, c := range a.Contributors {
			if c.Name == "ngram_rarity" {
				return c.Value
			}
		}
		return -1
	}

	commonAnomaly, uncommonAnomaly, rareAnomaly, unseenAnomaly := scoreFor(common), scoreFor(uncommon), scoreFor(rare), scoreFor(unseen)
	commonRarity, uncommonRarity, rareRarity := rarityValue(commonAnomaly), rarityValue(uncommonAnomaly), rarityValue(rareAnomaly)

	if commonRarity < 0 || uncommonRarity < 0 || rareRarity < 0 {
		t.Fatalf("expected ngram_rarity on all three seen 3-grams: common=%v uncommon=%v rare=%v", commonRarity, uncommonRarity, rareRarity)
	}
	if !(commonRarity < uncommonRarity && uncommonRarity < rareRarity) {
		t.Fatalf("rarity ordering violated: common=%v, uncommon=%v, rare=%v — want common < uncommon < rare", commonRarity, uncommonRarity, rareRarity)
	}

	if hasSignal(unseenAnomaly.Contributors, "ngram_rarity") {
		t.Error("unseen 3-gram carried ngram_rarity, want it to carry only ngram_deviation")
	}
	if !hasSignal(unseenAnomaly.Contributors, "ngram_deviation") {
		t.Error("unseen 3-gram did not carry ngram_deviation")
	}
	if hasSignal(commonAnomaly.Contributors, "ngram_deviation") {
		t.Error("common (seen) 3-gram carried ngram_deviation, want only ngram_rarity")
	}
}

// TestScoreNGramRarityColdStart mirrors TestScoreTransitionRarityColdStart
// one level up: below MinNGramObservations, ngram_rarity does not fire
// at all, regardless of distribution.
func TestScoreNGramRarityColdStart(t *testing.T) {
	grandparent := fingerprint.Compute(stable("auth-service"))
	predecessor := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	destA := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "a", TargetName: "customer-db", Environment: "production",
	})
	destB := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "b", TargetName: "customer-db", Environment: "production",
	})

	cfg := anomaly.DefaultConfig()
	cfg.NGramRarityWeight = 0.8
	const minSupport = 20
	cfg.MinNGramObservations = minSupport

	tests := []struct {
		name          string
		totalOutgoing int
		wantFire      bool
	}{
		{"zero observations", 0, false},
		{"one observation", 1, false},
		{"minSupport - 1", minSupport - 1, false},
		{"minSupport exactly", minSupport, true},
		{"minSupport + 1", minSupport + 1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b baseline.Baseline
			if tt.totalOutgoing == 0 {
				b = baseline.New(testKey)
			} else if tt.totalOutgoing == 1 {
				b = ngramBaseline(grandparent, predecessor, []fingerprint.Fingerprint{destB}, []int{1})
			} else {
				b = ngramBaseline(grandparent, predecessor, []fingerprint.Fingerprint{destA, destB}, []int{tt.totalOutgoing - 1, 1})
			}

			eventTime := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
			if !b.LastFingerprintTime.IsZero() {
				eventTime = b.LastFingerprintTime.Add(time.Second)
			}
			feat := features.Features{Stable: destB.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
			got := anomaly.Score(feat, destB, b, cfg)

			fired := hasSignal(got.Contributors, "ngram_rarity")
			if fired != tt.wantFire {
				t.Errorf("ngram_rarity fired = %v, want %v (totalOutgoing=%d, minSupport=%d)", fired, tt.wantFire, tt.totalOutgoing, minSupport)
			}
		})
	}
}

// TestScoreMatchesDocumentedFormulaForNGramRarity reproduces the exact
// frequency/rarity arithmetic, mirroring
// TestScoreMatchesDocumentedFormulaForTransitionRarity one level up.
func TestScoreMatchesDocumentedFormulaForNGramRarity(t *testing.T) {
	grandparent := fingerprint.Compute(stable("auth-service"))
	predecessor := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	common := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "common", TargetName: "customer-db", Environment: "production",
	})
	rare := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "rare", TargetName: "customer-db", Environment: "production",
	})

	// 96 + 4 = 100 total; rare's frequency is exactly 4/100 = 0.04.
	b := ngramBaseline(grandparent, predecessor, []fingerprint.Fingerprint{common, rare}, []int{96, 4})

	cfg := anomaly.DefaultConfig()
	cfg.NGramRarityWeight = 0.5

	eventTime := b.LastFingerprintTime.Add(time.Second)
	feat := features.Features{Stable: rare.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
	got := anomaly.Score(feat, rare, b, cfg)

	wantFrequency := 4.0 / 100.0
	wantValue := 1 - wantFrequency // 0.96

	var gotValue float64
	found := false
	for _, c := range got.Contributors {
		if c.Name == "ngram_rarity" {
			gotValue = c.Value
			found = true
		}
	}
	if !found {
		t.Fatalf("Contributors = %+v, want ngram_rarity", got.Contributors)
	}
	if diff := gotValue - wantValue; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("ngram_rarity Value = %v, want %v (1 - 4/100)", gotValue, wantValue)
	}

	// rare's own Count is 4 (< MinObservations=20), so categorical_novelty
	// also genuinely fires here — accounted for explicitly, mirroring
	// TestScoreMatchesDocumentedFormulaForTransitionRarity's own approach.
	familiarity := min(4.0/float64(cfg.MinObservations), 1)
	noveltyContribution := (1 - familiarity) * cfg.NoveltyWeight
	rarityContribution := wantValue * cfg.NGramRarityWeight
	wantScore := 1 - (1-noveltyContribution)*(1-rarityContribution)
	if diff := got.Score - wantScore; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula)", got.Score, wantScore)
	}
}

// TestScoreNGramRarityNeverExceedsBounds mirrors
// TestScoreTransitionRarityNeverExceedsBounds one level up: across a
// range of totals, ngram_rarity's Value must always land in [0,1] —
// never negative, never >1, never NaN, never Inf.
func TestScoreNGramRarityNeverExceedsBounds(t *testing.T) {
	grandparent := fingerprint.Compute(stable("auth-service"))
	predecessor := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	cfg := anomaly.DefaultConfig()
	cfg.NGramRarityWeight = 1.0
	cfg.MinNGramObservations = 1

	for _, total := range []int{1, 2, 5, 20, 1000} {
		dest := fingerprint.Compute(features.StableFeatures{
			ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
			OperationName: fmt.Sprintf("dest-%d", total), TargetName: "customer-db", Environment: "production",
		})
		b := ngramBaseline(grandparent, predecessor, []fingerprint.Fingerprint{dest}, []int{total})

		eventTime := b.LastFingerprintTime.Add(time.Second)
		feat := features.Features{Stable: dest.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
		got := anomaly.Score(feat, dest, b, cfg)

		for _, c := range got.Contributors {
			if c.Name != "ngram_rarity" {
				continue
			}
			if math.IsNaN(c.Value) || math.IsInf(c.Value, 0) {
				t.Errorf("total=%d: ngram_rarity Value = %v, want a finite number", total, c.Value)
			}
			if c.Value < 0 || c.Value > 1 {
				t.Errorf("total=%d: ngram_rarity Value = %v, want in [0,1]", total, c.Value)
			}
		}
	}
}

// TestScoreCombinedSequenceSignalsRemainBounded is task 027 §27/§47's
// own required proof: an event that is both a genuinely novel/rare
// pairwise transition AND a genuinely novel/rare 3-gram (which happens
// whenever the final pairwise hop itself has never been observed — see
// transitionSignal/ngramDeviationSignal's own doc comments) can fire
// transition_deviation and ngram_deviation simultaneously. combine()'s
// noisy-OR is not redesigned by this task (per its own explicit
// instruction) — this test only proves the existing formula keeps the
// combined result finite and within [0,1] even when every sequence
// signal fires at once, the same guarantee every other multi-signal
// combination in this package already has by construction (see
// combine()'s own clamp on each contribution).
func TestScoreCombinedSequenceSignalsRemainBounded(t *testing.T) {
	grandparent := fingerprint.Compute(stable("auth-service"))
	predecessor := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "read_customer", TargetName: "customer-db", Environment: "production",
	})
	novelDest := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "delete_customer", TargetName: "customer-db", Environment: "production",
	})

	// A predecessor with real outgoing-transition history (so
	// transition_rarity/ngram_rarity's minimum-support gates are
	// irrelevant here — this scenario exercises the *deviation*
	// signals, both of which fire on count==0 regardless of support),
	// but never observed leading into novelDest at all, at either the
	// pairwise or 3-gram level.
	otherDest := fingerprint.Compute(features.StableFeatures{
		ActorType: event.ActorTypeService, OperationCategory: event.OperationCategoryDB,
		OperationName: "other", TargetName: "customer-db", Environment: "production",
	})
	b := ngramBaseline(grandparent, predecessor, []fingerprint.Fingerprint{otherDest}, []int{30})

	cfg := anomaly.DefaultConfig()
	cfg.TransitionWeight = 1.0
	cfg.TransitionRarityWeight = 1.0
	cfg.NGramWeight = 1.0
	cfg.NGramRarityWeight = 1.0
	cfg.NoveltyWeight = 1.0
	cfg.SensitiveTargetFloor = map[string]float64{"customer-db": 0.9}

	eventTime := b.LastFingerprintTime.Add(time.Second)
	feat := features.Features{Stable: novelDest.Stable, Volatile: features.VolatileFeatures{Timestamp: eventTime}}
	got := anomaly.Score(feat, novelDest, b, cfg)

	if !hasSignal(got.Contributors, "transition_deviation") {
		t.Errorf("Contributors = %+v, want transition_deviation to also fire (setup check)", got.Contributors)
	}
	if !hasSignal(got.Contributors, "ngram_deviation") {
		t.Errorf("Contributors = %+v, want ngram_deviation to also fire (setup check)", got.Contributors)
	}
	if math.IsNaN(got.Score) || math.IsInf(got.Score, 0) {
		t.Fatalf("Score = %v with every sequence signal firing at once, want a finite number", got.Score)
	}
	if got.Score < 0 || got.Score > 1 {
		t.Fatalf("Score = %v with every sequence signal firing at once, want in [0,1]", got.Score)
	}
}

func hasSignal(signals []anomaly.Signal, name string) bool {
	for _, s := range signals {
		if s.Name == name {
			return true
		}
	}
	return false
}
