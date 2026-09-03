package fingerprint_test

import (
	"testing"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

func BenchmarkCompute(b *testing.B) {
	stable := stableFeatures(event.ActorTypeService, event.OperationCategoryHTTP, "POST /payment", "payment-db", "production")

	b.ReportAllocs()
	for b.Loop() {
		_ = fingerprint.Compute(stable)
	}
}
