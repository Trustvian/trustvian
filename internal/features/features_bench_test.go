package features_test

import (
	"testing"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/features"
)

func BenchmarkExtract(b *testing.B) {
	e := baseEvent(event.OperationCategoryHTTP, "POST /payment", map[string]any{
		features.AttrDurationMS: float64(120.5),
		features.AttrError:      false,
	})

	b.ReportAllocs()
	for b.Loop() {
		_ = features.Extract(e)
	}
}
