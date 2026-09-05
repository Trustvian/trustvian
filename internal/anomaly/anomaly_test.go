package anomaly_test

import (
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

func TestScoreFingerprintIDMatchesComputedFingerprint(t *testing.T) {
	feat := features.Features{Stable: stable("payment-db")}
	fp := fingerprint.Compute(feat.Stable)
	want := fp.ID

	got := anomaly.Score(feat, fp, baseline.New(testKey), anomaly.DefaultConfig())

	if got.FingerprintID != want {
		t.Fatalf("FingerprintID = %q, want %q", got.FingerprintID, want)
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
