package fingerprint

import (
	"hash/fnv"
	"strconv"
	"testing"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
)

// TestComputeIncludesVersionInHash proves fingerprintVersion genuinely
// participates in the digest — not merely documented as doing so — by
// hand-computing the same FNV-1a hash Compute should produce (version
// followed by the six stable fields, each NUL-terminated by writeField)
// and asserting Compute's ID matches it exactly.
func TestComputeIncludesVersionInHash(t *testing.T) {
	stable := features.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryHTTP,
		OperationName:     "GET /x",
		TargetName:        "svc-a",
		TargetCategory:    event.TargetCategoryInternal,
		Environment:       "prod",
	}

	h := fnv.New64a()
	writeField(h, fingerprintVersion)
	writeField(h, string(stable.ActorType))
	writeField(h, string(stable.OperationCategory))
	writeField(h, stable.OperationName)
	writeField(h, stable.TargetName)
	writeField(h, string(stable.TargetCategory))
	writeField(h, stable.Environment)
	want := strconv.FormatUint(h.Sum64(), 16)

	got := Compute(stable)
	if got.ID != want {
		t.Errorf("Compute(...).ID = %q, want %q (hand-computed with version+6 fields)", got.ID, want)
	}
}
