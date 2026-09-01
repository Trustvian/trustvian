// Package store defines the narrow persistence port Baseline data flows
// through, and an in-memory implementation of it. It intentionally exposes
// only the two operations the pipeline actually needs — read the current
// snapshot for a Key, apply one observation — rather than a generic
// repository interface.
package store

import (
	"context"
	"sync"
	"time"

	"github.com/Trustvian/trustvian/internal/baseline"
	"github.com/Trustvian/trustvian/internal/features"
	"github.com/Trustvian/trustvian/internal/fingerprint"
)

// Store is the port the Engine reads and updates Baseline data through.
// Implementations must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Get returns the current Baseline for key. If no observation has
	// ever been recorded for key, it returns an empty baseline.New(key)
	// and false.
	Get(ctx context.Context, key baseline.Key) (baseline.Baseline, bool)

	// Observe applies one observation of fp and vol to the Baseline for
	// key at time now, and returns the resulting Baseline.
	Observe(ctx context.Context, key baseline.Key, fp fingerprint.Fingerprint, vol features.VolatileFeatures, now time.Time) (baseline.Baseline, error)
}

// shard holds the current Baseline for exactly one Key, behind its own
// lock. Scoping the lock to a single Key rather than a fixed hash bucket
// means concurrent Observe/Get calls for different actors never contend
// with each other — contention only exists between calls for the same
// actor.
type shard struct {
	mu       sync.RWMutex
	baseline baseline.Baseline
}

// InMemory is a Store backed by an in-memory map, safe for concurrent use.
// It is the MVP's only Baseline persistence: baselines do not survive
// process restarts. The Store port exists so a persistent implementation
// can be added later without changing any caller.
type InMemory struct {
	mu     sync.RWMutex
	shards map[baseline.Key]*shard
}

// NewInMemory returns an empty InMemory store.
func NewInMemory() *InMemory {
	return &InMemory{shards: make(map[baseline.Key]*shard)}
}

func (s *InMemory) Get(ctx context.Context, key baseline.Key) (baseline.Baseline, bool) {
	sh, ok := s.lookup(key)
	if !ok {
		return baseline.New(key), false
	}
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	return sh.baseline, true
}

func (s *InMemory) Observe(ctx context.Context, key baseline.Key, fp fingerprint.Fingerprint, vol features.VolatileFeatures, now time.Time) (baseline.Baseline, error) {
	sh := s.getOrCreate(key)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.baseline = sh.baseline.Observe(fp, vol, now)
	return sh.baseline, nil
}

func (s *InMemory) lookup(key baseline.Key) (*shard, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh, ok := s.shards[key]
	return sh, ok
}

func (s *InMemory) getOrCreate(key baseline.Key) *shard {
	if sh, ok := s.lookup(key); ok {
		return sh
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh, ok := s.shards[key]; ok {
		return sh
	}
	sh := &shard{baseline: baseline.New(key)}
	s.shards[key] = sh
	return sh
}

var _ Store = (*InMemory)(nil)
