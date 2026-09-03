package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
	"github.com/Trustvian/trustvian/internal/store"
)

var testKey = baseline.Key{ActorID: "svc-payment", Environment: "production"}

func testFingerprint() fingerprint.Fingerprint {
	return fingerprint.Compute(features.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryHTTP,
		OperationName:     "POST /payment",
		TargetName:        "payment-db",
		Environment:       "production",
	})
}

func TestInMemoryGetMissingKey(t *testing.T) {
	s := store.NewInMemory()

	b, ok := s.Get(context.Background(), testKey)
	if ok {
		t.Fatalf("Get() ok = true for a never-observed key")
	}
	if b.Key != testKey {
		t.Fatalf("Get() Key = %+v, want %+v", b.Key, testKey)
	}
	if len(b.Fingerprints) != 0 {
		t.Fatalf("Get() on missing key returned non-empty Fingerprints: %v", b.Fingerprints)
	}
}

func TestInMemoryObserveThenGet(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	observed, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{HasLatency: true, Latency: 10 * time.Millisecond}, time.Now())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if observed.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("Observe() Count = %d, want 1", observed.Fingerprints[fp.ID].Count)
	}

	got, ok := s.Get(ctx, testKey)
	if !ok {
		t.Fatalf("Get() ok = false after Observe")
	}
	if got.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("Get() Count = %d, want 1", got.Fingerprints[fp.ID].Count)
	}
}

func TestInMemoryGetSnapshotUnaffectedByLaterObserve(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	snapshot, _ := s.Get(ctx, testKey)

	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	if got := snapshot.Fingerprints[fp.ID].Count; got != 1 {
		t.Fatalf("earlier Get() snapshot changed after a later Observe: Count = %d, want 1", got)
	}
}

func TestInMemoryObserveConcurrentSameKey(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	const goroutines = 50
	const perGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range perGoroutine {
				if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
					t.Errorf("Observe() error = %v", err)
				}
			}
		}()
	}
	wg.Wait()

	got, ok := s.Get(ctx, testKey)
	if !ok {
		t.Fatalf("Get() ok = false after concurrent Observe calls")
	}
	want := uint64(goroutines * perGoroutine)
	if got := got.Fingerprints[fp.ID].Count; got != want {
		t.Fatalf("Count = %d after %d concurrent Observe calls, want %d (lost update)", got, want, want)
	}
}

func TestInMemoryObserveConcurrentDistinctKeys(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	const actors = 100
	var wg sync.WaitGroup
	wg.Add(actors)
	for i := range actors {
		go func(i int) {
			defer wg.Done()
			key := baseline.Key{ActorID: fmt.Sprintf("actor-%d", i), Environment: "production"}
			if _, err := s.Observe(ctx, key, fp, features.VolatileFeatures{}, time.Now()); err != nil {
				t.Errorf("Observe() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	for i := range actors {
		key := baseline.Key{ActorID: fmt.Sprintf("actor-%d", i), Environment: "production"}
		b, ok := s.Get(ctx, key)
		if !ok {
			t.Fatalf("actor-%d: Get() ok = false", i)
		}
		if got := b.Fingerprints[fp.ID].Count; got != 1 {
			t.Fatalf("actor-%d: Count = %d, want 1", i, got)
		}
	}
}

func TestInMemoryFreezeMakesObserveANoOp(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	s.Freeze(ctx, testKey)
	if !s.IsFrozen(ctx, testKey) {
		t.Fatalf("IsFrozen() = false immediately after Freeze()")
	}

	before, _ := s.Get(ctx, testKey)
	got, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now())
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got.Fingerprints[fp.ID].Count != before.Fingerprints[fp.ID].Count {
		t.Fatalf("Observe() on a frozen key changed Count: before=%d after=%d",
			before.Fingerprints[fp.ID].Count, got.Fingerprints[fp.ID].Count)
	}
	if got.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("Count = %d after a frozen Observe, want unchanged at 1", got.Fingerprints[fp.ID].Count)
	}

	// Get must still return full history while frozen.
	current, ok := s.Get(ctx, testKey)
	if !ok || current.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("Get() while frozen = %+v, ok=%v, want full history intact", current, ok)
	}
}

func TestInMemoryUnfreezeResumesLearning(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()

	s.Freeze(ctx, testKey)
	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if b, _ := s.Get(ctx, testKey); b.Fingerprints[fp.ID].Count != 0 {
		t.Fatalf("Count = %d while frozen, want 0 (never learned)", b.Fingerprints[fp.ID].Count)
	}

	s.Unfreeze(ctx, testKey)
	if s.IsFrozen(ctx, testKey) {
		t.Fatalf("IsFrozen() = true after Unfreeze()")
	}
	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if b, _ := s.Get(ctx, testKey); b.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("Count = %d after Unfreeze, want 1", b.Fingerprints[fp.ID].Count)
	}
}

func TestInMemoryIsFrozenDefaultsFalse(t *testing.T) {
	s := store.NewInMemory()
	ctx := context.Background()

	if s.IsFrozen(ctx, testKey) {
		t.Fatalf("IsFrozen() = true for a key that was never touched, want false")
	}
}

func TestInMemoryFreezeIsPerKey(t *testing.T) {
	s := store.NewInMemory()
	fp := testFingerprint()
	ctx := context.Background()
	otherKey := baseline.Key{ActorID: "other-actor", Environment: "production"}

	s.Freeze(ctx, testKey)

	if _, err := s.Observe(ctx, otherKey, fp, features.VolatileFeatures{}, time.Now()); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if b, _ := s.Get(ctx, otherKey); b.Fingerprints[fp.ID].Count != 1 {
		t.Fatalf("Count = %d for an unrelated, unfrozen key, want 1 (freeze must not leak across keys)", b.Fingerprints[fp.ID].Count)
	}
}
