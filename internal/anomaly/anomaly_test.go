package anomaly_test

import (
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

// matureBaseline returns a Baseline where fp has been observed `count`
// times, each with latencyMS latency and no errors.
func matureBaseline(fp fingerprint.Fingerprint, count int, latencyMS float64) baseline.Baseline {
	b := baseline.New(testKey)
	for range count {
		b = b.Observe(fp, features.VolatileFeatures{
			HasLatency: true,
			Latency:    time.Duration(latencyMS * float64(time.Millisecond)),
		}, time.Now())
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
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{HasLatency: true, Latency: 500 * time.Millisecond},
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
		Stable:   sensitiveStable,
		Volatile: features.VolatileFeatures{HasLatency: true, Latency: 5 * time.Millisecond},
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
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{HasLatency: true, Latency: 10 * time.Millisecond},
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

func TestScoreErrorAgainstCleanBaselineIsAnomalous(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	b := baseline.New(testKey)
	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{Error: false}, time.Now())
	}

	feat := features.Features{
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{Error: true},
	}

	got := anomaly.Score(feat, fp, b, anomaly.DefaultConfig())

	if !hasSignal(got.Contributors, "error_deviation") {
		t.Fatalf("Contributors = %+v, want error_deviation", got.Contributors)
	}
}

func TestScoreErrorAgainstErrorProneBaselineIsNotAnomalous(t *testing.T) {
	fp := fingerprint.Compute(stable("payment-db"))
	b := baseline.New(testKey)
	for range 50 {
		b = b.Observe(fp, features.VolatileFeatures{Error: true}, time.Now())
	}

	feat := features.Features{
		Stable:   stable("payment-db"),
		Volatile: features.VolatileFeatures{Error: true},
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

	b := baseline.New(testKey)
	for range 10 { // 10/20 => familiarity 0.5 => novelty value 0.5
		b = b.Observe(fp, features.VolatileFeatures{}, time.Now())
	}

	feat := features.Features{Stable: stable("secrets-manager")}
	got := anomaly.Score(feat, fp, b, cfg)

	noveltyContribution := 0.5 * cfg.NoveltyWeight
	floorContribution := 0.5 * 1.0
	want := 1 - (1-noveltyContribution)*(1-floorContribution)

	if diff := got.Score - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want %v (documented noisy-OR formula)", got.Score, want)
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
