package store_test

// This file is the Store *contract* suite: the behavioral guarantees
// every store.Store implementation must satisfy, asserted against every
// implementation from one place rather than re-written per backend.
//
// It exists because v0.8 introduces a third implementation
// (PostgreSQL — see docs/ROADMAP.md § v0.8 and
// docs/adr/0018-production-store-boundary-and-postgresql-direction.md),
// and "production-grade persistence" is only meaningful against a
// written-down contract. Before this suite, the contract lived
// implicitly in two parallel sets of near-identical tests
// (TestInMemory* / TestFileStore*), which is fine for two backends and
// actively misleading for three: a new implementation could pass its
// own hand-written tests while quietly differing on a guarantee the
// Engine depends on.
//
// Those existing per-implementation tests are deliberately kept, not
// replaced — they cover genuinely implementation-specific behavior this
// suite cannot (FileStore's restart durability, its atomic-rename
// write, its deliberate non-persistence of freeze state). The overlap
// on shared guarantees is intentional: this suite is the contract,
// those are the backend's own specifics.
//
// Adding an implementation means adding one line to storeFactories —
// nothing else. If it cannot pass unmodified, either the
// implementation is wrong or the contract changed; both are decisions
// worth forcing into the open, which is the whole point.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/event"
	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
	"github.com/Trustvian/trustvian/internal/store"
	"github.com/Trustvian/trustvian/internal/store/postgres"
)

// storeFactory builds a fresh, empty Store for one contract subtest.
// Every call must return an independent store with no carried-over
// state — contract subtests assume they start empty.
type storeFactory func(t *testing.T) store.Store

// postgresDSNEnv gates the PostgreSQL entry in storeFactories. Unset —
// the default for any developer without a local database — means the
// contract runs against the two in-process implementations only, so
// `go test ./...` never requires a server. Set, it adds PostgreSQL to
// the *same* assertions, rather than a parallel suite: that shared
// execution is the whole point of this file (see ADR 0018 § Integration
// testing, and docs/storage-guide.md for the one-line docker command).
const postgresDSNEnv = "TRUSTVIAN_TEST_POSTGRES_DSN"

// storeFactories enumerates every implementation the contract applies
// to. Adding a backend is one entry here and nothing else — the
// assertions below are untouched by this task, which is what makes them
// a contract rather than a description of whatever the backends happen
// to do.
func storeFactories() map[string]storeFactory {
	factories := map[string]storeFactory{
		"InMemory": func(t *testing.T) store.Store {
			t.Helper()
			return store.NewInMemory()
		},
		"FileStore": func(t *testing.T) store.Store {
			t.Helper()
			s, err := store.NewFileStore(filepath.Join(t.TempDir(), "baseline.json"))
			if err != nil {
				t.Fatalf("NewFileStore() error = %v", err)
			}
			return s
		},
	}

	if dsn := os.Getenv(postgresDSNEnv); dsn != "" {
		factories["PostgreSQL"] = func(t *testing.T) store.Store {
			t.Helper()
			return newCleanPostgresStore(t, dsn)
		}
	}
	return factories
}

// newCleanPostgresStore returns a PostgreSQL-backed Store whose tables
// live in a schema private to this test, satisfying storeFactory's
// "starts empty" requirement.
//
// Isolation is by schema, not by truncating a shared table. The first
// version of this helper truncated, on the reasoning that the contract
// subtests run sequentially — which is true, and which missed that
// `go test ./...` runs *separate packages' binaries concurrently*.
// internal/store/postgres also exercises PostgreSQL, so its truncation
// and this one wiped each other's rows mid-test, producing "lost update"
// failures against a provably correct implementation. A private schema
// removes the shared resource rather than trying to time-share it — and
// as a bonus, these tests no longer destroy data in a database that
// happens to hold some.
//
// Emptying baselines still has no business being a method on the
// production Store type: there is no legitimate production caller for
// it, and the schema-per-test approach means tests do not need one.
func newCleanPostgresStore(t *testing.T, dsn string) store.Store {
	t.Helper()

	s, err := postgres.NewStore(context.Background(), postgres.Config{
		DSN: isolatedSchemaDSN(t, dsn),
	})
	if err != nil {
		t.Fatalf("postgres.NewStore() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// contractTime is a fixed base timestamp. Contract subtests that care
// about ordering advance from it explicitly; those that do not reuse it
// unchanged, so no contract assertion depends on wall-clock timing.
var contractTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// otherContractFingerprint is a second, distinct Fingerprint — needed
// wherever the contract concerns a *relationship* between two
// fingerprints (a transition) rather than one in isolation.
// testFingerprint (store_test.go) supplies the first.
func otherContractFingerprint() fingerprint.Fingerprint {
	return fingerprint.Compute(features.StableFeatures{
		ActorType:         event.ActorTypeService,
		OperationCategory: event.OperationCategoryDB,
		OperationName:     "SELECT accounts",
		TargetName:        "payment-db",
		Environment:       "production",
	})
}

func TestStoreContract(t *testing.T) {
	for name, newStore := range storeFactories() {
		t.Run(name, func(t *testing.T) {
			t.Run("GetMissingKeyReturnsEmptyBaselineAndFalse", func(t *testing.T) {
				contractGetMissingKey(t, newStore)
			})
			t.Run("ObserveThenGetReturnsTheObservation", func(t *testing.T) {
				contractObserveThenGet(t, newStore)
			})
			t.Run("ObserveReturnsSameBaselineGetReturns", func(t *testing.T) {
				contractObserveReturnValueMatchesGet(t, newStore)
			})
			t.Run("ObserveIsIncrementalNotOverwrite", func(t *testing.T) {
				contractObserveIsIncremental(t, newStore)
			})
			t.Run("GetResultIsAnImmutableSnapshot", func(t *testing.T) {
				contractGetIsImmutableSnapshot(t, newStore)
			})
			t.Run("DistinctKeysAreIsolated", func(t *testing.T) {
				contractDistinctKeysIsolated(t, newStore)
			})
			t.Run("ConcurrentObserveSameKeyLosesNoUpdates", func(t *testing.T) {
				contractConcurrentSameKeyLosesNoUpdates(t, newStore)
			})
			t.Run("ConcurrentObserveDistinctKeysLosesNoUpdates", func(t *testing.T) {
				contractConcurrentDistinctKeysLoseNoUpdates(t, newStore)
			})
			t.Run("SequenceStateSurvivesThePort", func(t *testing.T) {
				contractSequenceStateSurvives(t, newStore)
			})
		})
	}
}

// contractGetMissingKey pins the documented missing-key behavior: a
// zero-value-but-keyed Baseline plus false, never a nil/invalid value
// and never an error. internal/anomaly relies on this — it scores a
// never-before-seen actor against exactly this empty Baseline rather
// than special-casing absence.
func contractGetMissingKey(t *testing.T, newStore storeFactory) {
	s := newStore(t)

	bl, ok := s.Get(context.Background(), testKey)
	if ok {
		t.Errorf("Get(missing) ok = true, want false")
	}
	if bl.Key != testKey {
		t.Errorf("Get(missing) Baseline.Key = %+v, want %+v — the returned empty Baseline must still carry its Key", bl.Key, testKey)
	}
	if len(bl.Fingerprints) != 0 {
		t.Errorf("Get(missing) len(Fingerprints) = %d, want 0", len(bl.Fingerprints))
	}
}

func contractObserveThenGet(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()

	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, contractTime); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	bl, ok := s.Get(ctx, testKey)
	if !ok {
		t.Fatalf("Get() after Observe: ok = false, want true")
	}
	if got := bl.Fingerprints[fp.ID].Count; got != 1 {
		t.Errorf("Fingerprints[%q].Count = %d, want 1", fp.ID, got)
	}
}

// contractObserveReturnValueMatchesGet pins that Observe's return value
// is the post-update state, not the pre-update state — the Engine reads
// it directly in some paths rather than re-issuing a Get.
func contractObserveReturnValueMatchesGet(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()

	returned, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, contractTime)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	fetched, ok := s.Get(ctx, testKey)
	if !ok {
		t.Fatalf("Get() ok = false, want true")
	}

	if !reflect.DeepEqual(returned, fetched) {
		t.Errorf("Observe() return value differs from the subsequent Get():\nObserve: %+v\nGet:     %+v", returned, fetched)
	}
}

// contractObserveIsIncremental is the guarantee that makes a correct
// transactional implementation possible at all: Observe applies *one
// observation* to whatever state exists, it does not write back a
// caller-supplied Baseline. See ADR 0018 § Why the narrow port makes
// lost updates avoidable.
func contractObserveIsIncremental(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()
	now := contractTime

	const observations = 5
	for range observations {
		if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, now); err != nil {
			t.Fatalf("Observe() error = %v", err)
		}
		now = now.Add(time.Second)
	}

	bl, _ := s.Get(ctx, testKey)
	if got := bl.Fingerprints[fp.ID].Count; got != observations {
		t.Errorf("Count = %d after %d Observe calls, want %d — Observe must accumulate, not overwrite", got, observations, observations)
	}
}

// contractGetIsImmutableSnapshot pins the property that lets a caller
// read a Baseline without holding any lock: the value Get returned must
// not change underneath it when a later Observe lands. This is what
// baseline.Baseline's copy-on-write design exists to provide, asserted
// here at the port rather than only inside internal/baseline.
func contractGetIsImmutableSnapshot(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()

	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, contractTime); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	snapshot, _ := s.Get(ctx, testKey)
	countAtSnapshot := snapshot.Fingerprints[fp.ID].Count

	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, contractTime.Add(time.Second)); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	if got := snapshot.Fingerprints[fp.ID].Count; got != countAtSnapshot {
		t.Errorf("a Baseline previously returned by Get changed after a later Observe: Count %d -> %d", countAtSnapshot, got)
	}
}

func contractDistinctKeysIsolated(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()

	other := baseline.Key{ActorID: "svc-other", Environment: "production"}
	if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, contractTime); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}

	bl, ok := s.Get(ctx, other)
	if ok {
		t.Errorf("Get(other key) ok = true, want false — one actor's observation must not create another's Baseline")
	}
	if len(bl.Fingerprints) != 0 {
		t.Errorf("Get(other key) len(Fingerprints) = %d, want 0", len(bl.Fingerprints))
	}
}

// contractConcurrentSameKeyLosesNoUpdates is the single most important
// assertion in this file, and the one a naive database implementation
// fails: N goroutines each applying M observations to the *same* Key
// must produce exactly N*M observations, with none lost.
//
// A backend that implements Observe as a non-atomic
// read-then-compute-then-write cycle will silently drop updates here —
// two writers read the same Count, each computes Count+1, and one
// write clobbers the other. That is precisely the failure mode ADR 0018
// requires the PostgreSQL implementation to prevent (row-level locking
// or equivalent), and "a production database that still loses
// behavioral learning under concurrency is not production-grade."
//
// A fixed timestamp is used on purpose: Baseline.Observe increments
// Count unconditionally, while its interval/transition statistics are
// ordering-guarded, so Count is the one field with an exact,
// order-independent expected value under concurrency.
func contractConcurrentSameKeyLosesNoUpdates(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()

	const (
		writers           = 8
		observationsEach  = 25
		wantTotalObserved = writers * observationsEach
	)

	var wg sync.WaitGroup
	errs := make(chan error, writers*observationsEach)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range observationsEach {
				if _, err := s.Observe(ctx, testKey, fp, features.VolatileFeatures{}, contractTime); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Observe() error = %v", err)
	}

	bl, ok := s.Get(ctx, testKey)
	if !ok {
		t.Fatalf("Get() ok = false, want true")
	}
	if got := bl.Fingerprints[fp.ID].Count; got != wantTotalObserved {
		t.Errorf("Count = %d after %d concurrent observations, want %d — %d update(s) were lost",
			got, wantTotalObserved, wantTotalObserved, wantTotalObserved-int(got))
	}
}

func contractConcurrentDistinctKeysLoseNoUpdates(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	fp := testFingerprint()

	const (
		keys             = 8
		observationsEach = 10
	)

	var wg sync.WaitGroup
	errs := make(chan error, keys*observationsEach)
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := baseline.Key{ActorID: fmt.Sprintf("actor-%d", i), Environment: "production"}
			for range observationsEach {
				if _, err := s.Observe(ctx, key, fp, features.VolatileFeatures{}, contractTime); err != nil {
					errs <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Observe() error = %v", err)
	}

	for i := range keys {
		key := baseline.Key{ActorID: fmt.Sprintf("actor-%d", i), Environment: "production"}
		bl, ok := s.Get(ctx, key)
		if !ok {
			t.Fatalf("Get(%s) ok = false, want true", key.ActorID)
		}
		if got := bl.Fingerprints[fp.ID].Count; got != observationsEach {
			t.Errorf("%s: Count = %d, want %d", key.ActorID, got, observationsEach)
		}
	}
}

// contractSequenceStateSurvives pins that v0.6/v0.7 behavioral state
// (transition counters, and the two-element history window they are
// derived from) round-trips through the port intact. A backend that
// persists only Fingerprints and silently drops
// LastFingerprintID/PredecessorCounts would pass every other
// assertion here while breaking sequence detection outright.
func contractSequenceStateSurvives(t *testing.T, newStore storeFactory) {
	s := newStore(t)
	ctx := context.Background()
	first, second := testFingerprint(), otherContractFingerprint()
	now := contractTime

	if _, err := s.Observe(ctx, testKey, first, features.VolatileFeatures{}, now); err != nil {
		t.Fatalf("Observe(first) error = %v", err)
	}
	now = now.Add(time.Second)
	if _, err := s.Observe(ctx, testKey, second, features.VolatileFeatures{}, now); err != nil {
		t.Fatalf("Observe(second) error = %v", err)
	}

	bl, ok := s.Get(ctx, testKey)
	if !ok {
		t.Fatalf("Get() ok = false, want true")
	}
	if got := bl.Fingerprints[second.ID].PredecessorCounts[first.ID]; got != 1 {
		t.Errorf("PredecessorCounts[first] for second = %d, want 1 — transition state must survive the port", got)
	}
	if bl.LastFingerprintID != second.ID {
		t.Errorf("LastFingerprintID = %q, want %q", bl.LastFingerprintID, second.ID)
	}
	if got := bl.Fingerprints[first.ID].OutgoingTransitionTotal; got != 1 {
		t.Errorf("first.OutgoingTransitionTotal = %d, want 1", got)
	}
}

// TestStoreContractImplementationsAgreeOnLogicalState is §17's
// cross-implementation equivalence check: given the identical sequence
// of Store operations, every implementation must arrive at the same
// logical Baseline. This is a stronger statement than "each passes the
// contract individually" — it catches a backend that is internally
// self-consistent but diverges from the others (a different rounding,
// a dropped field, a different bound).
func TestStoreContractImplementationsAgreeOnLogicalState(t *testing.T) {
	ctx := context.Background()
	first, second := testFingerprint(), otherContractFingerprint()

	// One fixed operation sequence, replayed identically per backend.
	replay := func(s store.Store) baseline.Baseline {
		t.Helper()
		now := contractTime
		for range 3 {
			if _, err := s.Observe(ctx, testKey, first, features.VolatileFeatures{HasLatency: true, Latency: 10 * time.Millisecond}, now); err != nil {
				t.Fatalf("Observe(first) error = %v", err)
			}
			now = now.Add(time.Second)
			if _, err := s.Observe(ctx, testKey, second, features.VolatileFeatures{Error: true}, now); err != nil {
				t.Fatalf("Observe(second) error = %v", err)
			}
			now = now.Add(time.Second)
		}
		bl, ok := s.Get(ctx, testKey)
		if !ok {
			t.Fatalf("Get() ok = false, want true")
		}
		return bl
	}

	var reference baseline.Baseline
	var referenceName string
	for name, newStore := range storeFactories() {
		got := replay(newStore(t))
		if referenceName == "" {
			reference, referenceName = got, name
			continue
		}
		if !reflect.DeepEqual(got, reference) {
			t.Errorf("%s and %s diverge on logical state for an identical operation sequence:\n%s: %+v\n%s: %+v",
				name, referenceName, name, got, referenceName, reference)
		}
	}
}
