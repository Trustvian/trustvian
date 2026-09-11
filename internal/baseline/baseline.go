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

// hourActivityAlpha is the EWMA smoothing factor for HourActivity —
// deliberately much slower than emaAlpha, not an arbitrary second
// constant. Every observation updates all 24 HourActivity buckets (see
// observe), but only one of them matches the current hour; the other
// 23 only ever decay. With emaAlpha's 0.2, a bucket that is hit exactly
// once every 24 observations (uniform hour-of-day traffic — the
// textbook "no real time pattern" case) decays to ~0.2% of its peak
// value between hits and rebounds to ~20% right after one, a >200x
// swing depending purely on *when* it happens to be read relative to
// its own last hit — which would make even genuinely patternless
// traffic look sharply time-anomalous, purely as a measurement-phase
// artifact, not a real behavioral signal
// (TestFingerprintStatsHourActivityUniformTraffic pins this down: it
// failed under emaAlpha before this constant was introduced).
// hourActivityAlpha=0.02 keeps that same 24-step round-trip decay to
// within about ±0.01 of the true 1/24 uniform share — small enough
// that the anomaly signal built on it (see internal/anomaly) reads as
// only mildly, not severely, anomalous for uniform traffic, and small
// enough in absolute terms to be a non-issue given the signal ships
// with a zero default weight (see anomaly.Config.TimePatternWeight)
// until an operator has calibrated it against real traffic anyway.
// The tradeoff is slower adaptation to genuine hour-of-day drift
// (effective memory of roughly 100 observations, vs. emaAlpha's ~9) —
// appropriate here, since a real hour-of-day pattern is a weeks-scale
// phenomenon, not something that should shift on the last 5-10 calls.
const hourActivityAlpha = 0.02

// maxPredecessors bounds FingerprintStats.PredecessorCounts: the number
// of distinct predecessor Fingerprint.IDs tracked for one destination
// fingerprint. This is a resource-exhaustion bound, not a statistical
// one — Fingerprints itself (the map this bounds a per-entry map
// within) is already unbounded by design (see
// TestObserveUnboundedFingerprintsDoesNotPanic in engine_test.go and
// docs/SECURITY.md § Resource exhaustion), but a per-destination
// predecessor map compounds that: an attacker who can vary the
// *previous* fingerprint on every call (trivial — Fingerprint.ID is
// derived from Event fields the caller controls) could otherwise grow
// one FingerprintStats entry's PredecessorCounts without bound. 64 is
// generous for real traffic — a real actor's distinct predecessor
// actions are typically a handful to a few dozen (an actor's own
// behavioral repertoire, not per-event entropy) — while keeping the
// worst case (64 entries × a small fixed key/value size) trivial
// memory, matching the "bounded, not unlimited" mandate for any new
// sequence-aware state (see docs/adr/0010-bounded-process-local-sequence-state.md).
const maxPredecessors = 64

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

	// LastObserved is the latest observation timestamp seen for this
	// Fingerprint. It advances monotonically: an observation whose
	// timestamp precedes it (clock skew, out-of-order delivery) is still
	// counted, but does not pull LastObserved backwards. See observe.
	LastObserved time.Time

	// LatencyObservations is the subset of Count that carried latency
	// data (VolatileFeatures.HasLatency). LatencyMean/LatencyVariance are
	// meaningless when this is zero.
	LatencyObservations uint64
	LatencyMean         float64 // EWMA mean latency, in nanoseconds
	LatencyVariance     float64 // EWMA variance, in nanoseconds^2

	// IntervalObservations is the number of times an inter-observation
	// interval has been recorded for this Fingerprint. It is at most
	// Count-1: the first observation has no prior LastObserved to measure
	// from, and observations that do not strictly follow LastObserved are
	// skipped entirely (see observe). IntervalMean/IntervalVariance are
	// meaningless when this is zero.
	IntervalObservations uint64
	IntervalMean         float64 // EWMA mean inter-observation interval, in nanoseconds
	IntervalVariance     float64 // EWMA variance, in nanoseconds^2

	// ErrorRate is the EWMA-smoothed proportion of observations that
	// carried an error, in [0,1].
	ErrorRate float64

	// TimePatternObservations counts every observation this
	// FingerprintStats has recorded an hour-of-day sample for — always
	// equal to Count going forward, but tracked separately so a
	// FingerprintStats loaded from a persisted file written before this
	// field existed (HourActivity unmarshals to its zero value) is
	// correctly treated as immature for this specific signal, exactly
	// like a brand-new fingerprint, rather than as fully mature with a
	// suspiciously empty distribution. See internal/anomaly's
	// time-pattern signal, gated on this field rather than on Count.
	TimePatternObservations uint64
	// HourActivity is an EWMA-smoothed distribution over UTC
	// hour-of-day (index 0-23): each bucket estimates the fraction of
	// this Fingerprint's traffic that historically falls in that hour.
	// A fingerprint with no time-of-day pattern converges toward
	// 1/24 in every bucket; one concentrated at a specific hour
	// converges toward 1.0 there and 0.0 elsewhere.
	HourActivity [24]float64

	// Stable is the shape this Fingerprint represents, retained for
	// explainability so a consumer never has to recompute it.
	Stable features.StableFeatures

	// PredecessorCounts records, for this destination Fingerprint, how
	// many times each distinct predecessor Fingerprint.ID has
	// immediately preceded it in this actor's observed event order —
	// the minimum state a transition-deviation signal needs (see
	// internal/anomaly's transition_deviation) without yet computing a
	// Markov transition probability (deliberately deferred; see
	// docs/ROADMAP.md § v0.6). A nil map (the zero value, exactly like
	// a persisted file written before this field existed) means "no
	// transition into this fingerprint observed yet" — the correct,
	// safe cold-start default, not a distinguishable error state.
	//
	// Bounded at maxPredecessors distinct entries: once at that bound,
	// a genuinely new predecessor is not added (see recordPredecessor)
	// — existing entries keep accumulating normally. This is a
	// deliberately simple "first N distinct predecessors win" policy,
	// not LRU: the failure mode at the bound is that a not-yet-tracked
	// predecessor always reads as "unseen" (maximally novel), which is
	// the conservative, safe direction to fail in — it never causes an
	// already-legitimate, tracked transition to be silently forgotten.
	PredecessorCounts map[string]uint64
}

// recordPredecessor increments counts[predecessor], creating counts if
// nil, but never grows counts past maxPredecessors distinct keys — see
// PredecessorCounts's own doc comment for why this bound exists and why
// this specific eviction policy (deterministic refusal to add a new key
// past the bound, not LRU) was chosen.
//
// Like Baseline.Observe/FingerprintStats.observe, this never mutates
// counts in place: a map, unlike HourActivity's fixed-size array, is a
// reference type, and a shallow maps.Copy of a Fingerprints map (as
// Baseline.Observe performs) only copies the FingerprintStats struct
// values, not the maps a value like PredecessorCounts points into — an
// in-place counts[predecessor]++ here would silently corrupt whatever
// earlier Baseline snapshot (e.g. one a concurrent Store.Get caller is
// still holding) shares that same underlying map.
func recordPredecessor(counts map[string]uint64, predecessor string) map[string]uint64 {
	if _, tracked := counts[predecessor]; !tracked && len(counts) >= maxPredecessors {
		return counts
	}

	next := make(map[string]uint64, len(counts)+1)
	maps.Copy(next, counts)
	next[predecessor]++
	return next
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

// predecessor is the caller-supplied Fingerprint.ID that immediately
// preceded this observation for the same actor, or "" if there was
// none (the actor's first-ever observation) or the ordering guard in
// Baseline.Observe determined this observation does not validly follow
// one (see there for why). observe itself performs no ordering check
// of its own — by the time predecessor reaches here, that decision has
// already been made once, by the one caller (Baseline.Observe).
func (s FingerprintStats) observe(stable features.StableFeatures, vol features.VolatileFeatures, now time.Time, predecessor string) FingerprintStats {
	if s.Count == 0 {
		s.FirstObserved = now
	} else if now.After(s.LastObserved) {
		// Only a strictly forward-moving timestamp carries interval
		// information. An event that does not follow the last one —
		// clock skew, out-of-order delivery, or a deliberately
		// backdated event from an untrusted source — would otherwise
		// fold a negative (or zero) interval into the EWMA, which is a
		// baseline-poisoning primitive: such an event is typically
		// decided observe_only and so is eligible for learning, and one
		// of them is enough to drag IntervalMean below zero and make
		// every subsequent on-time event look anomalous. Skipping it
		// records "no interval information this time" rather than the
		// misleading data point a zero-or-negative interval would be.
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
	// LastObserved is the latest timestamp seen for this Fingerprint, not
	// the timestamp of the most recent Observe call: it never regresses.
	// Letting an out-of-order event pull it backwards would corrupt the
	// *next* interval too (the following in-order event would measure
	// from a timestamp that is not actually the most recent observation),
	// which would defeat the guard above. It also keeps IsStale honest —
	// a backdated arrival must not make a fingerprint look staler than
	// the freshest evidence we hold for it.
	if now.After(s.LastObserved) {
		s.LastObserved = now
	}
	s.Stable = stable

	// HourActivity tracks the same EWMA-of-indicator shape ErrorRate
	// already uses, applied per hour-of-day bucket instead of a single
	// scalar: every observation nudges the bucket matching now's UTC
	// hour toward 1 and every other bucket toward 0. Unlike the
	// interval/latency EWMAs above, this uses now (the event's own
	// timestamp), not a derived value, and is unaffected by the
	// out-of-order guard above — an out-of-order event's hour is still
	// real information about when this fingerprint fires, even though
	// its *interval* isn't trustworthy.
	hour := now.UTC().Hour()
	if s.TimePatternObservations == 0 {
		for h := range s.HourActivity {
			s.HourActivity[h] = 0
		}
		s.HourActivity[hour] = 1
	} else {
		for h := range s.HourActivity {
			sample := 0.0
			if h == hour {
				sample = 1.0
			}
			s.HourActivity[h] += hourActivityAlpha * (sample - s.HourActivity[h])
		}
	}
	s.TimePatternObservations++

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

	if predecessor != "" {
		s.PredecessorCounts = recordPredecessor(s.PredecessorCounts, predecessor)
	}

	return s
}

// Baseline is the statistical model for a single Key: one FingerprintStats
// entry per distinct Fingerprint the actor has produced.
type Baseline struct {
	Key          Key
	Fingerprints map[string]FingerprintStats

	// LastObserved is when this Baseline was last written — the `now` of
	// the most recent Observe call, whatever its ordering. This is
	// deliberately *not* the same rule as FingerprintStats.LastObserved,
	// which tracks the latest evidence for one Fingerprint and never
	// regresses because interval statistics are measured from it.
	// Nothing measures anything from this field; it records write
	// recency for the whole Baseline.
	LastObserved time.Time

	// LastFingerprintID is the Fingerprint.ID of this actor's most
	// recently observed event — the "previous action" a transition
	// (LastFingerprintID -> the next Fingerprint.ID observed) is formed
	// from. "" means no prior observation exists yet to form a
	// transition from (this actor's first-ever event, or a freshly
	// constructed Baseline) — the correct, safe default, not an error
	// state.
	//
	// Unlike LastObserved above, this *does* follow an ordering guard
	// (paired with LastFingerprintTime below), for the identical reason
	// FingerprintStats.LastObserved's own guard exists: an out-of-order
	// or backdated event must not be allowed to silently rewrite "what
	// the previous action was" for the *next* legitimate event's
	// transition to be measured against — see Observe.
	LastFingerprintID string

	// LastFingerprintTime is the timestamp associated with
	// LastFingerprintID — never regresses; see Observe for the guard
	// that enforces this. Zero (time.Time{}) exactly when
	// LastFingerprintID is "".
	LastFingerprintTime time.Time
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
//
// This is also where LastFingerprintID/LastFingerprintTime advance,
// under the same ordering guard FingerprintStats.observe already
// applies to interval statistics: now must strictly follow
// LastFingerprintTime for this observation to (a) be treated as a
// valid transition *from* b.LastFingerprintID at all, and (b) become
// the new predecessor for whatever observation follows it. An
// out-of-order or backdated now is folded into fp's own
// FingerprintStats (Count, etc.) as usual, but contributes no
// transition information in either direction — exactly the same
// "absence of information, not a misleading data point" stance
// FingerprintStats.observe's own interval guard takes.
func (b Baseline) Observe(fp fingerprint.Fingerprint, vol features.VolatileFeatures, now time.Time) Baseline {
	validTransition := b.LastFingerprintID != "" && now.After(b.LastFingerprintTime)
	predecessor := ""
	if validTransition {
		predecessor = b.LastFingerprintID
	}

	next := make(map[string]FingerprintStats, len(b.Fingerprints)+1)
	maps.Copy(next, b.Fingerprints)
	next[fp.ID] = next[fp.ID].observe(fp.Stable, vol, now, predecessor)

	lastFingerprintID := b.LastFingerprintID
	lastFingerprintTime := b.LastFingerprintTime
	if b.LastFingerprintID == "" || now.After(b.LastFingerprintTime) {
		lastFingerprintID = fp.ID
		lastFingerprintTime = now
	}

	return Baseline{
		Key:                 b.Key,
		Fingerprints:        next,
		LastObserved:        now,
		LastFingerprintID:   lastFingerprintID,
		LastFingerprintTime: lastFingerprintTime,
	}
}
