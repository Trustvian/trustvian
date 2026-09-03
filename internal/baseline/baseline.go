// Package baseline is the statistical model of an actor's expected
// behavior: for each Fingerprint an actor has produced, how often it's
// been seen, and — when volatile signals are present — its typical latency
// and error rate. Anomaly detection (internal/anomaly) compares a new
// event's Fingerprint and VolatileFeatures against this model.
//
// Baseline is a pure, immutable value type: Observe never mutates the
// receiver, it returns a new Baseline reflecting the update. This makes a
// Baseline value returned to a caller (e.g. by internal/store) a safe,
// permanently-valid snapshot — no lock needs to be held while it's read.
// Concurrency-safe storage and mutation of the "current" Baseline for a
// given Key is internal/store's job, not this package's.
package baseline

import (
	"maps"
	"math"
	"time"

	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

// emaAlpha is the exponential-moving-average smoothing factor used for
// latency mean/variance and error rate. It reconciles two requirements:
// O(1)-memory streaming statistics (no raw samples retained, in the spirit
// of Welford's online algorithm) and decay, so that older observations
// progressively lose influence and legitimate behavioral drift is absorbed
// without an unbounded all-time window or a manual reset.
//
// 0.2 favors the last ~5-10 observations, a reasonable default until
// production data justifies tuning it.
const emaAlpha = 0.2

// Key scopes a Baseline to a single actor within a single deployment
// environment. Scoping by environment from the start — even though the
// OSS engine is single-tenant — keeps the data model tenant-shaped, so a
// future multi-tenant Enterprise feature is an access-control addition,
// not a data migration.
type Key struct {
	ActorID     string
	Environment string
}

// FingerprintStats is the learned statistical profile for one Fingerprint:
// how many times it's been observed, and — when available — its latency
// and error-rate behavior.
type FingerprintStats struct {
	// Count is the maturity counter: the number of times this Fingerprint
	// has been observed. Callers (anomaly detection) decide what count
	// constitutes "known" or "mature" — this package only counts.
	Count uint64

	FirstObserved time.Time
	LastObserved  time.Time

	// LatencyObservations is the subset of Count that carried latency
	// data (VolatileFeatures.HasLatency). LatencyMean/LatencyVariance are
	// meaningless when this is zero.
	LatencyObservations uint64
	LatencyMean         float64 // EWMA mean latency, in nanoseconds
	LatencyVariance     float64 // EWMA variance, in nanoseconds^2

	// IntervalObservations is the number of times an inter-observation
	// interval has been recorded for this Fingerprint (Count-1 once
	// Count>0, since the first observation has no prior LastObserved to
	// measure from). IntervalMean/IntervalVariance are meaningless when
	// this is zero.
	IntervalObservations uint64
	IntervalMean         float64 // EWMA mean inter-observation interval, in nanoseconds
	IntervalVariance     float64 // EWMA variance, in nanoseconds^2

	// ErrorRate is the EWMA-smoothed proportion of observations that
	// carried an error, in [0,1].
	ErrorRate float64

	// Stable is the shape this Fingerprint represents, retained for
	// explainability so a consumer never has to recompute it.
	Stable features.StableFeatures
}

// LatencyMeanDuration returns LatencyMean as a time.Duration.
func (s FingerprintStats) LatencyMeanDuration() time.Duration {
	return time.Duration(s.LatencyMean)
}

// LatencyStdDevDuration returns the standard deviation implied by
// LatencyVariance, as a time.Duration. It is zero when no latency
// observation has been recorded.
func (s FingerprintStats) LatencyStdDevDuration() time.Duration {
	if s.LatencyObservations == 0 {
		return 0
	}
	return time.Duration(math.Sqrt(s.LatencyVariance))
}

// IsStale reports whether s has not been observed within maxAge of now.
// A FingerprintStats with no observations at all (Count == 0) is never
// stale — that's cold start, a different concept from staleness (see
// internal/anomaly's Confidence for cold start): staleness is about a
// fingerprint that *was* known and hasn't been seen in a while, not one
// that was never known at all.
//
// This package only reports staleness; it does not act on it (no
// automatic expiration or deletion) — deciding what to do with a stale
// entry, if anything, is a caller's policy decision, matching the
// existing "Baseline only counts" design (see Count's doc comment).
func (s FingerprintStats) IsStale(now time.Time, maxAge time.Duration) bool {
	if s.Count == 0 {
		return false
	}
	return now.Sub(s.LastObserved) > maxAge
}

func (s FingerprintStats) observe(stable features.StableFeatures, vol features.VolatileFeatures, now time.Time) FingerprintStats {
	if s.Count == 0 {
		s.FirstObserved = now
	} else {
		intervalNS := float64(now.Sub(s.LastObserved))
		if s.IntervalObservations == 0 {
			s.IntervalMean = intervalNS
			s.IntervalVariance = 0
		} else {
			delta := intervalNS - s.IntervalMean
			s.IntervalMean += emaAlpha * delta
			s.IntervalVariance = (1 - emaAlpha) * (s.IntervalVariance + emaAlpha*delta*delta)
		}
		s.IntervalObservations++
	}
	s.Count++
	s.LastObserved = now
	s.Stable = stable

	if vol.HasLatency {
		latencyNS := float64(vol.Latency)
		if s.LatencyObservations == 0 {
			s.LatencyMean = latencyNS
			s.LatencyVariance = 0
		} else {
			delta := latencyNS - s.LatencyMean
			s.LatencyMean += emaAlpha * delta
			s.LatencyVariance = (1 - emaAlpha) * (s.LatencyVariance + emaAlpha*delta*delta)
		}
		s.LatencyObservations++
	}

	errSample := 0.0
	if vol.Error {
		errSample = 1.0
	}
	if s.Count == 1 {
		s.ErrorRate = errSample
	} else {
		s.ErrorRate += emaAlpha * (errSample - s.ErrorRate)
	}

	return s
}

// Baseline is the statistical model for a single Key: one FingerprintStats
// entry per distinct Fingerprint the actor has produced.
type Baseline struct {
	Key          Key
	Fingerprints map[string]FingerprintStats
	LastObserved time.Time
}

// New returns an empty Baseline for key, ready to be passed to Observe.
func New(key Key) Baseline {
	return Baseline{Key: key}
}

// Observe returns a new Baseline reflecting one additional observation of
// fp with volatile signals vol at time now. b is not modified: the
// returned value has its own Fingerprints map, so any Baseline value a
// caller already holds (e.g. from a prior Store.Get) remains a valid,
// unaffected snapshot.
func (b Baseline) Observe(fp fingerprint.Fingerprint, vol features.VolatileFeatures, now time.Time) Baseline {
	next := make(map[string]FingerprintStats, len(b.Fingerprints)+1)
	maps.Copy(next, b.Fingerprints)
	next[fp.ID] = next[fp.ID].observe(fp.Stable, vol, now)

	return Baseline{
		Key:          b.Key,
		Fingerprints: next,
		LastObserved: now,
	}
}
