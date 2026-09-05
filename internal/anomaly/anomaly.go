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
		MinObservations:      20,
		LatencyZThreshold:    3.0,
		FrequencyZThreshold:  3.0,
		NoveltyWeight:        1.0,
		LatencyWeight:        0.6,
		ErrorWeight:          0.8,
		FrequencyWeight:      0,
		SensitiveTargetFloor: map[string]float64{},
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
