// This file is task 034's mandatory external-consumer proof (see
// docs/tasks/034-production-store-contract-and-public-boundary.md):
// examples/ is a genuinely separate Go module (examples/go.mod, with
// only a `replace` directive back to this repository), so a passing
// `go test ./...` here demonstrates empirically that an external OSS
// consumer can select and use durable persistence through public API
// alone. It imports only event, config, and the root trustvian
// package — never internal/*, which Go's own internal/ import
// restriction would refuse to compile regardless of intent.
//
// That restriction is exactly why this proof is necessary rather than
// ceremonial: store.Store's methods reference internal types, so before
// task 034 there was no expression an external module could write to
// obtain one. A test that compiles at all is already most of the claim.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
)

// TestPublicConfigSelectsDurableStoreAcrossRestart is the end-to-end
// claim: public config → CompileStorage → WithStore → Engine, learn,
// discard the Engine, rebuild from the same file, and find the learned
// state still there.
func TestPublicConfigSelectsDurableStoreAcrossRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "baseline.json")
	ctx := context.Background()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tick := func() time.Time { clock = clock.Add(time.Minute); return clock }

	first, err := newEngineOn(statePath)
	if err != nil {
		t.Fatalf("newEngineOn: %v", err)
	}

	const observations = 12
	for i := range observations {
		result, err := first.Analyze(ctx, paymentEvent("warm-up", tick()))
		if err != nil {
			t.Fatalf("Analyze %d: %v", i, err)
		}
		if _, err := first.Observe(ctx, result); err != nil {
			t.Fatalf("Observe %d: %v", i, err)
		}
	}

	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file not written: %v", err)
	}

	// A completely fresh Engine over the same file — nothing shared in
	// memory with the first.
	second, err := newEngineOn(statePath)
	if err != nil {
		t.Fatalf("newEngineOn (restart): %v", err)
	}

	result, err := second.Analyze(ctx, paymentEvent("after-restart", tick()))
	if err != nil {
		t.Fatalf("Analyze after restart: %v", err)
	}

	if result.Anomaly.Confidence == 0 {
		t.Fatalf("Anomaly.Confidence = 0 after restart — the persisted baseline was not loaded")
	}
	if got := result.Fingerprint.ID; got == "" {
		t.Fatalf("Fingerprint.ID is empty")
	}
}

// TestPublicConfigMemoryStoreDoesNotPersist is the control for the test
// above: the identical code path with `type: memory` must *not* carry
// state across engines. Without this, a passing restart test could be
// explained by something other than the file store actually working.
func TestPublicConfigMemoryStoreDoesNotPersist(t *testing.T) {
	ctx := context.Background()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tick := func() time.Time { clock = clock.Add(time.Minute); return clock }

	newMemoryEngine := func() *trustvian.Engine {
		t.Helper()
		s, err := config.CompileStorage(config.StorageConfig{
			Version: config.StorageSchemaVersionV1,
			Type:    config.StorageTypeMemory,
		})
		if err != nil {
			t.Fatalf("CompileStorage(memory): %v", err)
		}
		return trustvian.NewEngine(trustvian.WithStore(s))
	}

	first := newMemoryEngine()
	for range 12 {
		result, err := first.Analyze(ctx, paymentEvent("warm-up", tick()))
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if _, err := first.Observe(ctx, result); err != nil {
			t.Fatalf("Observe: %v", err)
		}
	}

	second := newMemoryEngine()
	result, err := second.Analyze(ctx, paymentEvent("after-restart", tick()))
	if err != nil {
		t.Fatalf("Analyze after restart: %v", err)
	}
	if result.Anomaly.Confidence != 0 {
		t.Errorf("Anomaly.Confidence = %v for a fresh in-memory store, want 0 — memory storage must not persist", result.Anomaly.Confidence)
	}
}

// TestPublicConfigUnimplementedBackendFailsClosed proves the fail-closed
// guarantee is visible to external consumers too, not just enforced
// internally: requesting a recognized-but-unimplemented backend yields
// an error and no Store, never a silently substituted non-durable one.
func TestPublicConfigUnimplementedBackendFailsClosed(t *testing.T) {
	s, err := config.CompileStorage(config.StorageConfig{
		Version: config.StorageSchemaVersionV1,
		Type:    config.StorageTypePostgres,
	})

	if !errors.Is(err, config.ErrStorageTypeNotImplemented) {
		t.Errorf("CompileStorage() err = %v, want %v", err, config.ErrStorageTypeNotImplemented)
	}
	if s != nil {
		t.Errorf("CompileStorage() returned a non-nil Store for an unimplemented backend — an external caller must never receive a silent substitute")
	}
}
