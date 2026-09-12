// Package anomaly scores how much a single event's Features deviate from
// an actor's Baseline. It is deterministic: no machine learning, every
// contributing signal is retained on the result, and the combination
// formula is documented rather than tuned by feel.
//
// Score reports two independent numbers, and callers must weigh both:
//
//   - Score is how anomalous the event looks, in [0,1].
//   - Confidence is how much the Baseline actually supports that
//     judgment, in [0,1] — driven by how mature (how many times
//     previously observed) the specific Fingerprint is.
//
// A brand-new Fingerprint scores as maximally novel (Score near 1) but
// with Confidence 0: this package does not itself suppress the score to
// avoid a "false BLOCK", because collapsing two different questions
// ("how different is this" and "how much should you trust that
// judgment") into one number would destroy exactly the information a
// downstream Trust/Policy stage needs to make that call safely. Cold
// start is handled by consuming Confidence downstream, not by capping
// Score here.
package anomaly

import (
	"fmt"
	"math"
	"time"

	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

// Config holds the thresholds and weights Score combines signals with.
// Every constant that shapes the anomaly score lives here, in one place,
// rather than scattered as magic numbers — so the formula stays
// documented and testable rather than arbitrary.
type Config struct {
	// MinObservations is how many times a Fingerprint must have been
	// observed to count as fully mature. Below this, both the
	// categorical-novelty signal and Confidence ramp linearly from 0 to
	// their full value as Count approaches MinObservations.
	MinObservations uint64

	// LatencyZThreshold is the |z-score| at which the latency-deviation
	// signal reaches its full strength (1.0). Must be > 0.
	LatencyZThreshold float64

	// FrequencyZThreshold is the |z-score| at which the frequency-deviation
	// signal (deviation of the current inter-observation interval from the
	// baseline's typical interval) reaches its full strength (1.0). Must be > 0.
	FrequencyZThreshold float64

	// Weight fields scale a signal's raw [0,1] value before it enters
	// the noisy-OR combination in Score. A weight of 1.0 means the
	// signal alone can drive the combined score to that same value.
	NoveltyWeight float64
	LatencyWeight float64
	ErrorWeight   float64

	// FrequencyWeight defaults to 0, which makes frequency_deviation an
	// opt-in signal: it is still computed and still reported in
	// Anomaly.Contributors for explainability, but contributes nothing
	// to Score until an operator sets a non-zero weight. This mirrors
	// SensitiveTargetFloor, which likewise ships empty and inert.
	//
	// The reason is calibration, not doubt about the mechanism. The
	// signal measures a z-score of the current inter-event interval
	// against an EWMA whose stddev is whatever jitter that fingerprint's
	// traffic happens to carry — and on a service with only a few
	// milliseconds of natural jitter around a ten-second cadence, a
	// completely ordinary event a few milliseconds off the mean already
	// reaches |z| > 3 and clamps the signal to 1.0. With a 0.6 weight
	// that alone was enough to carry a fully-familiar actor to
	// RiskHigh (see TestAnalyzeOrdinaryCadenceJitterDoesNotElevateRisk),
	// which is a false positive on routine traffic — and, under a
	// risk-gated policy, a BLOCK that is then ineligible for learning.
	// There is no single default that is simultaneously correct for a
	// cron job, a chatty RPC poller, and a human-driven UI.
	//
	// To enable it: measure your own fleet's inter-request jitter for
	// the fingerprints you care about (FingerprintStats.IntervalMean and
	// the stddev implied by IntervalVariance), set FrequencyZThreshold
	// above the |z| your ordinary traffic actually produces, and only
	// then raise FrequencyWeight — starting low (e.g. 0.3) and watching
	// the resulting Decision mix before going higher. See
	// examples/frequency-abuse for a worked opt-in.
	FrequencyWeight float64

	// TimePatternWeight defaults to 0, for the identical reason
	// FrequencyWeight does: an EWMA-smoothed hour-of-day distribution
	// needs real traffic to calibrate against before it's trustworthy,
	// and a fingerprint that matures past MinObservations within a
	// single calendar day would show a spuriously sharp (but
	// statistically meaningless) pattern — see
	// docs/tasks/017-baseline-time-patterns.md's Non-Goals. The signal
	// is still computed and reported in Anomaly.Contributors; only its
	// contribution to Score is opt-in.
	TimePatternWeight float64

	// TransitionWeight defaults to 0, for the same reason
	// FrequencyWeight/TimePatternWeight do: a new v0.6 signal needs
	// real traffic to build up predecessor history before "never seen
	// this transition before" is a trustworthy judgment, not merely a
	// reflection of not having run long enough yet — see
	// docs/ROADMAP.md § v0.6 and docs/tasks/025-sequence-analysis-foundation.md's
	// Non-Goals. Existing v0.5 callers that construct a Config without
	// setting this field (or via DefaultConfig) get byte-for-byte
	// unchanged Score behavior: transition_deviation is still computed
	// and reported in Anomaly.Contributors, but contributes nothing to
	// Score until an operator opts in.
	TransitionWeight float64

	// MinTransitionObservations is how many valid outgoing transitions
	// a predecessor Fingerprint must have recorded
	// (baseline.FingerprintStats.OutgoingTransitionTotal) before
	// transition_rarity is computed for it at all — the identical
	// "don't mistake a tiny sample for evidence" role MinObservations
	// plays for categorical_novelty, applied to the denominator of a
	// transition frequency instead of a fingerprint's own maturity.
	// Below this, transition_rarity does not fire, full stop — not
	// "fires weakly." See
	// docs/adr/0011-transition-rarity-statistic-and-orientation.md §
	// Minimum support for why 20 (matching MinObservations's own
	// default) is a reasonable starting resolution.
	MinTransitionObservations uint64

	// TransitionRarityWeight defaults to 0, for the identical
	// "ships opt-in" reason every other v0.6 signal weight does: a
	// graded rarity reading needs real traffic to be meaningful, and
	// existing callers must see byte-for-byte unchanged Score output
	// unless they explicitly opt in. See
	// docs/tasks/026-transition-rarity.md's Non-Goals.
	TransitionRarityWeight float64

	// SensitiveTargetFloor maps a Target name to a minimum anomaly
	// contribution that always applies when that target is touched,
	// regardless of how familiar the Baseline is with it. This is what
	// prevents an actor from "training" the baseline into tolerating
	// access to a sensitive destination (e.g. a secrets manager) simply
	// by repeating it — familiarity alone can never erase this floor.
	SensitiveTargetFloor map[string]float64
}

// DefaultConfig returns reasonable MVP defaults.
//
// Two fields ship deliberately inert, because no default value for them
// is correct for every deployment: SensitiveTargetFloor is empty (callers
// populate it with the destinations their deployment considers
// sensitive), and FrequencyWeight is 0 (callers raise it once they have
// calibrated FrequencyZThreshold against their own traffic's jitter).
// Both mechanisms are fully implemented and reported in
// Anomaly.Contributors; only their contribution to Score is opt-in.
func DefaultConfig() Config {
	return Config{
		MinObservations:           20,
		LatencyZThreshold:         3.0,
		FrequencyZThreshold:       3.0,
		NoveltyWeight:             1.0,
		LatencyWeight:             0.6,
		ErrorWeight:               0.8,
		FrequencyWeight:           0,
		TimePatternWeight:         0,
		TransitionWeight:          0,
		MinTransitionObservations: 20,
		TransitionRarityWeight:    0,
		SensitiveTargetFloor:      map[string]float64{},
	}
}

// Signal is one contributor to the combined anomaly Score, retained for
// explainability. Only signals with a non-zero Value are reported.
type Signal struct {
	Name   string
	Value  float64 // raw signal strength in [0,1], before Weight is applied
	Weight float64
	Detail string // human-readable explanation of why this signal fired
}

// Anomaly is the result of scoring one event's Features against a
// Baseline.
type Anomaly struct {
	FingerprintID string
	Score         float64
	Confidence    float64
	Contributors  []Signal
}

// Score derives an Anomaly from feat and bl using cfg's thresholds and
// weights. It is a pure function: identical input always produces
// identical output.
//
// fp must be fingerprint.Compute(feat.Stable) — callers that also need
// the Fingerprint themselves (as Engine does, for Result.Fingerprint)
// compute it once and pass it in here, rather than Score recomputing it
// internally. On the Analyze hot path this halves the fingerprint-related
// allocations per call; see internal/fingerprint's benchmark and
// docs/PERFORMANCE.md.
func Score(feat features.Features, fp fingerprint.Fingerprint, bl baseline.Baseline, cfg Config) Anomaly {
	stats, known := bl.Fingerprints[fp.ID]

	familiarity := 0.0
	if known {
		if cfg.MinObservations == 0 {
			familiarity = 1.0
		} else {
			familiarity = min(float64(stats.Count)/float64(cfg.MinObservations), 1)
		}
	}

	var signals []Signal

	if noveltyValue := 1 - familiarity; noveltyValue > 0 {
		detail := "fingerprint never observed for this actor"
		if known {
			detail = fmt.Sprintf("fingerprint observed %d/%d times required for maturity", stats.Count, cfg.MinObservations)
		}
		signals = append(signals, Signal{Name: "categorical_novelty", Value: noveltyValue, Weight: cfg.NoveltyWeight, Detail: detail})
	}

	if feat.Volatile.HasLatency && known && stats.LatencyObservations > 0 {
		if s := latencySignal(feat.Volatile.Latency, stats, cfg); s.Value > 0 {
			signals = append(signals, s)
		}
	}

	// A non-positive interval means this event's Timestamp does not
	// follow the fingerprint's last recorded observation — clock skew,
	// out-of-order delivery, or a backdated event from an untrusted
	// source. That is an *absence* of interval information, not a
	// measurement of an impossibly fast one, so it is gated out for the
	// same reason IntervalObservations == 0 is: reporting a maximal
	// deviation here would let anyone who can backdate a timestamp
	// manufacture a frequency_deviation signal at will.
	// internal/baseline refuses to learn from such an interval too; this
	// is the read-side half of that guard.
	if known && stats.IntervalObservations > 0 {
		if interval := feat.Volatile.Timestamp.Sub(stats.LastObserved); interval > 0 {
			if s := frequencySignal(interval, stats, cfg); s.Value > 0 {
				signals = append(signals, s)
			}
		}
	}

	// TimePatternObservations, not Count, gates maturity here — a
	// FingerprintStats loaded from a store.FileStore file written
	// before this signal existed unmarshals HourActivity to its zero
	// value, and gating on Count alone would treat that as a fully
	// mature but suspiciously empty distribution (every hour reading
	// as maximally novel) rather than correctly re-accumulating fresh
	// evidence, exactly like a brand-new fingerprint. See
	// docs/tasks/017-baseline-time-patterns.md.
	if known && stats.TimePatternObservations >= cfg.MinObservations {
		hour := feat.Volatile.Timestamp.UTC().Hour()
		if s := timePatternSignal(hour, stats, cfg); s.Value > 0 {
			signals = append(signals, s)
		}
	}

	// bl.LastFingerprintID is the predecessor this event's Fingerprint
	// would transition from — "" means this actor's first-ever
	// observation, which has no predecessor to evaluate a transition
	// against, exactly like frequency_deviation's own
	// IntervalObservations==0 gate. The ordering guard mirrors
	// frequencySignal's: only a Timestamp that strictly follows
	// bl.LastFingerprintTime is treated as validly following that
	// predecessor — see baseline.Baseline.Observe's identical guard on
	// the write side, and docs/SECURITY.md § Sequence state for why.
	if bl.LastFingerprintID != "" && feat.Volatile.Timestamp.After(bl.LastFingerprintTime) {
		if s := transitionSignal(bl.LastFingerprintID, stats, cfg); s.Value > 0 {
			signals = append(signals, s)
		}
		if s := transitionRaritySignal(bl.LastFingerprintID, bl.Fingerprints[bl.LastFingerprintID], stats, cfg); s.Value > 0 {
			signals = append(signals, s)
		}
	}

	if feat.Volatile.Error {
		if s := errorSignal(known, stats, cfg); s.Value > 0 {
			signals = append(signals, s)
		}
	}

	if floor, ok := cfg.SensitiveTargetFloor[feat.Stable.TargetName]; ok && floor > 0 {
		signals = append(signals, Signal{
			Name:   "sensitive_target",
			Value:  floor,
			Weight: 1,
			Detail: fmt.Sprintf("target %q carries a fixed risk floor regardless of baseline familiarity", feat.Stable.TargetName),
		})
	}

	return Anomaly{
		FingerprintID: fp.ID,
		Score:         combine(signals),
		Confidence:    familiarity,
		Contributors:  signals,
	}
}

// latencySignal only formats its Detail string once it knows the signal
// actually contributes (Value > 0) — the common case, where latency
// matches the baseline exactly, must not pay for an explanation nobody
// will read.
func latencySignal(current time.Duration, stats baseline.FingerprintStats, cfg Config) Signal {
	stddev := float64(stats.LatencyStdDevDuration())
	mean := stats.LatencyMean
	currentNS := float64(current)
	nearZeroStdDev := stddev < float64(time.Microsecond)

	var value, z float64
	if nearZeroStdDev {
		if currentNS != mean {
			value = 1
		}
	} else {
		z = (currentNS - mean) / stddev
		if z < 0 {
			z = -z
		}
		value = min(z/cfg.LatencyZThreshold, 1)
	}

	if value == 0 {
		return Signal{Name: "latency_deviation", Weight: cfg.LatencyWeight}
	}

	detail := fmt.Sprintf("latency z-score %.2f (mean %s, stddev %s)", z, stats.LatencyMeanDuration(), stats.LatencyStdDevDuration())
	if nearZeroStdDev {
		detail = fmt.Sprintf("latency %s deviates from a stable baseline of %s (stddev ~0)", current, stats.LatencyMeanDuration())
	}
	return Signal{Name: "latency_deviation", Value: value, Weight: cfg.LatencyWeight, Detail: detail}
}

// frequencySignal only formats its Detail string once it knows the signal
// actually contributes (Value > 0) — mirrors latencySignal's cost discipline.
func frequencySignal(currentInterval time.Duration, stats baseline.FingerprintStats, cfg Config) Signal {
	stddev := math.Sqrt(stats.IntervalVariance)
	mean := stats.IntervalMean
	currentNS := float64(currentInterval)
	nearZeroStdDev := stddev < float64(time.Millisecond)

	var value, z float64
	if nearZeroStdDev {
		if currentNS != mean {
			value = 1
		}
	} else {
		z = (currentNS - mean) / stddev
		if z < 0 {
			z = -z
		}
		value = min(z/cfg.FrequencyZThreshold, 1)
	}

	if value == 0 {
		return Signal{Name: "frequency_deviation", Weight: cfg.FrequencyWeight}
	}

	detail := fmt.Sprintf("inter-event interval z-score %.2f (mean %s, stddev %s)", z, time.Duration(mean), time.Duration(stddev))
	if nearZeroStdDev {
		detail = fmt.Sprintf("interval %s deviates from a stable baseline of %s (stddev ~0)", currentInterval, time.Duration(mean))
	}
	return Signal{Name: "frequency_deviation", Value: value, Weight: cfg.FrequencyWeight, Detail: detail}
}

// timePatternSignal only formats its Detail string once it knows the
// signal actually contributes (Value > 0) — mirrors latencySignal's
// and frequencySignal's cost discipline.
//
// uniformShare is the HourActivity[hour] value a fingerprint with
// genuinely no time-of-day pattern converges toward (1/24, one bucket
// per hour) — not a threshold tuned by feel, but the direct
// mathematical reference point a uniform distribution over 24 buckets
// implies. value scales how far below that reference the current
// hour's bucket reads: at or above uniformShare, this hour is at least
// as active as an unpatterned fingerprint's average hour, so value is
// 0; at HourActivity[hour] == 0 (never observed at this hour), value
// is 1.
func timePatternSignal(hour int, stats baseline.FingerprintStats, cfg Config) Signal {
	const uniformShare = 1.0 / 24.0
	activity := max(stats.HourActivity[hour], 0)
	value := 1 - min(activity/uniformShare, 1)

	if value == 0 {
		return Signal{Name: "time_pattern_deviation", Weight: cfg.TimePatternWeight}
	}

	detail := fmt.Sprintf("hour-of-day %02d:00 UTC has historically accounted for %.1f%% of this fingerprint's traffic (uniform baseline: %.1f%%)", hour, activity*100, uniformShare*100)
	return Signal{Name: "time_pattern_deviation", Value: value, Weight: cfg.TimePatternWeight, Detail: detail}
}

// transitionSignal reports how novel the transition from predecessor
// into this destination fingerprint (destStats) is: 0 if this exact
// predecessor has led here at least once before (however few times —
// there is no "rare" threshold yet, only "seen" vs. "never seen"), 1
// if it never has. destStats.PredecessorCounts is read directly
// (nil-safe: a nil map read returns the zero value), so an entirely
// unknown destination fingerprint (destStats is FingerprintStats{})
// correctly reports maximal novelty too — every transition into a
// fingerprint that has never been observed at all is, by definition,
// itself never observed.
//
// This deliberately does not compute a transition *probability* (a
// Markov P(destination|predecessor)) or a frequency-based "rare"
// threshold — establishing reliable transition observation is this
// task's whole scope; see docs/ROADMAP.md § v0.6 for why probability
// estimation is a separate, later task.
func transitionSignal(predecessor string, destStats baseline.FingerprintStats, cfg Config) Signal {
	if destStats.PredecessorCounts[predecessor] > 0 {
		return Signal{Name: "transition_deviation", Weight: cfg.TransitionWeight}
	}
	return Signal{
		Name:   "transition_deviation",
		Value:  1,
		Weight: cfg.TransitionWeight,
		Detail: fmt.Sprintf("transition from fingerprint %s has never been observed leading to this one", predecessor),
	}
}

// transitionRaritySignal reports how rare an already-seen transition
// from predecessor into destStats is, as an empirical relative
// frequency — see
// docs/adr/0011-transition-rarity-statistic-and-orientation.md for the
// full statistical definition and, specifically, why
// P(destination | predecessor) — not P(predecessor | destination),
// which destStats.PredecessorCounts alone would give if misread — is
// the orientation computed here: predStats.OutgoingTransitionTotal is
// the total number of valid transitions where predecessor was the
// source, to *any* destination, making
// count/predStats.OutgoingTransitionTotal a genuine estimate of how
// often this specific destination follows predecessor, not how often
// this predecessor precedes this destination among its other
// predecessors.
//
// Mutually exclusive with transitionSignal by construction, not
// convention: this returns a zero-Value Signal whenever the
// transition has never been observed at all (count == 0) —
// transitionSignal already reports that case as transition_deviation,
// and collapsing the two would lose the seen-but-rare vs. never-seen
// distinction this task's own brief requires preserving.
//
// Gated on predStats.OutgoingTransitionTotal >=
// cfg.MinTransitionObservations: below that, a low frequency is more
// likely a tiny-sample artifact than genuine rarity, so the signal
// does not fire at all — not "fires weakly." The explicit
// OutgoingTransitionTotal == 0 check guards division by zero even if
// a caller misconfigures MinTransitionObservations to 0.
func transitionRaritySignal(predecessor string, predStats, destStats baseline.FingerprintStats, cfg Config) Signal {
	count := destStats.PredecessorCounts[predecessor]
	if count == 0 {
		return Signal{Name: "transition_rarity", Weight: cfg.TransitionRarityWeight}
	}
	if predStats.OutgoingTransitionTotal == 0 || predStats.OutgoingTransitionTotal < cfg.MinTransitionObservations {
		return Signal{Name: "transition_rarity", Weight: cfg.TransitionRarityWeight}
	}

	frequency := float64(count) / float64(predStats.OutgoingTransitionTotal)
	// frequency is always in (0, 1] by construction: count only ever
	// increments in lockstep with predStats.OutgoingTransitionTotal
	// for this exact (predecessor, destination) pair (see
	// baseline.Baseline.Observe), so count can never exceed it. The
	// clamp below is defensive, not expected to trigger — see ADR 0011
	// § Mapping frequency to a bounded signal value for why no
	// nonlinear transform is applied on top of it.
	value := max(min(1-frequency, 1), 0)
	if value == 0 {
		return Signal{Name: "transition_rarity", Weight: cfg.TransitionRarityWeight}
	}

	detail := fmt.Sprintf("transition observed %d/%d (%.2f%%) of this fingerprint's outgoing transitions", count, predStats.OutgoingTransitionTotal, frequency*100)
	return Signal{Name: "transition_rarity", Value: value, Weight: cfg.TransitionRarityWeight, Detail: detail}
}

func errorSignal(known bool, stats baseline.FingerprintStats, cfg Config) Signal {
	if !known {
		return Signal{
			Name:   "error_deviation",
			Value:  1,
			Weight: cfg.ErrorWeight,
			Detail: "error observed on a fingerprint with no baseline error history",
		}
	}
	return Signal{
		Name:   "error_deviation",
		Value:  1 - stats.ErrorRate,
		Weight: cfg.ErrorWeight,
		Detail: fmt.Sprintf("error observed against a baseline error rate of %.1f%%", stats.ErrorRate*100),
	}
}

// combine aggregates signals into a single [0,1] score via a noisy-OR:
// score = 1 - Π(1 - value_i * weight_i). Unlike an average, one severe
// signal (e.g. a sensitive-target floor) dominates the result rather than
// being diluted by several unrelated weak/benign signals.
func combine(signals []Signal) float64 {
	product := 1.0
	for _, s := range signals {
		contribution := max(min(s.Value*s.Weight, 1), 0)
		product *= 1 - contribution
	}
	return 1 - product
}
