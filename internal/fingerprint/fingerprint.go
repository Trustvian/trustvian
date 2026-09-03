// Package fingerprint derives a deterministic, content-addressed identity
// for a behavioral shape from an Event's stable features. Two events with
// the same stable dimensions — actor type, operation, target, environment —
// always produce the same Fingerprint ID, regardless of any volatile
// (per-event) data such as latency or timestamp. Baseline (see
// internal/baseline) tracks statistics keyed by this ID, so a Fingerprint
// never seen before for a given actor is itself a categorical-novelty
// signal for anomaly detection.
package fingerprint

import (
	"hash/fnv"
	"io"
	"strconv"

	"github.com/Trustvian/trustvian/internal/features"
)

// Fingerprint is the identity of a specific behavioral shape: a stable
// feature snapshot plus a deterministic ID derived from it.
type Fingerprint struct {
	ID     string
	Stable features.StableFeatures
}

// fingerprintVersion is written into the hash before every stable field.
// Bump it whenever the stable field set or hash algorithm changes (e.g.
// task 001's TargetCategory addition is what version "1" already
// includes) — this guarantees a composition change produces a disjoint ID
// space instead of silently reinterpreting old IDs under new semantics.
const fingerprintVersion = "1"

// Compute derives a Fingerprint from stable. It is a pure function of
// stable: identical input always produces an identical ID, and stable is
// never modified.
func Compute(stable features.StableFeatures) Fingerprint {
	h := fnv.New64a()
	writeField(h, fingerprintVersion)
	writeField(h, string(stable.ActorType))
	writeField(h, string(stable.OperationCategory))
	writeField(h, stable.OperationName)
	writeField(h, stable.TargetName)
	writeField(h, string(stable.TargetCategory))
	writeField(h, stable.Environment)

	return Fingerprint{
		ID:     strconv.FormatUint(h.Sum64(), 16),
		Stable: stable,
	}
}

// writeField writes s to w followed by a NUL separator, so that field
// boundaries can't shift into one another (e.g. Name="ab",Target="c" must
// not hash the same as Name="a",Target="bc").
func writeField(w io.Writer, s string) {
	_, _ = io.WriteString(w, s)
	_, _ = w.Write([]byte{0})
}
