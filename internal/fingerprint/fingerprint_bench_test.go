package fingerprint_test

import (
	"testing"

	"github.com/trustvian/trustvian/event"
	"github.com/trustvian/trustvian/internal/fingerprint"
)

func BenchmarkCompute(b *testing.B) {
	stable := stableFeatures(event.ActorTypeService, event.OperationCategoryHTTP, "POST /payment", "payment-db", "production")

	b.ReportAllocs()
	for b.Loop() {
		_ = fingerprint.Compute(stable)
	}
}
