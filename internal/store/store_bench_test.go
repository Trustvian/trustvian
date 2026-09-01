package store_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/store"
)

// BenchmarkInMemoryObserveSameKey measures worst-case lock contention: every
// goroutine observes the same actor's Baseline.
func BenchmarkInMemoryObserveSameKey(b *testing.B) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkInMemoryObserveDistinctKeys measures best-case contention: each
// goroutine observes its own actor's Baseline, so per-key locks never
// collide. Comparing this against BenchmarkInMemoryObserveSameKey
// quantifies the benefit of sharding the lock by Key.
func BenchmarkInMemoryObserveDistinctKeys(b *testing.B) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	var counter atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		key := baseline.Key{ActorID: fmt.Sprintf("actor-%d", counter.Add(1)), Environment: "production"}
		for pb.Next() {
			if _, err := s.Observe(ctx, key, fp, features.VolatileFeatures{}, time.Now()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
