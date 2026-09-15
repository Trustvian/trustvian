package trustvianprocessor_test

// Task 037's proof that the Collector processor can select the PostgreSQL
// Store through the canonical v0.8 storage boundary — and that it fails
// closed when it cannot.
//
// These tests exist because the reference Docker Compose deployment
// depends entirely on this path working. Before task 037 the processor had
// no storage configuration at all: it called NewEngine with at most
// WithPolicy, so every Collector deployment silently ran on the
// non-durable in-memory default and discarded every learned baseline on
// restart. That is the same gap task 034 found in the CLI, in the one
// runtime that is actually long-lived.
//
// Nothing here needs a database except the one test that says so.

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/processor"

	trustvianprocessor "trustvian-processor"
)

// TestConfigUnmarshalDecodesStorageBlock is the storage counterpart to
// TestConfigUnmarshalDecodesPolicyBlock, and proves the same specific
// mechanism works for a second document: Collector's confmap decoder
// (mapstructure, case-sensitive tag matching, ErrorUnused) can carry a
// real `storage:` block — including its nested `postgres:` block and
// snake_case keys — into Config.Storage.
//
// This matters because Collector's decoder only reads `mapstructure` tags
// and config.StorageConfig only carries `yaml` tags. Embedding
// StorageConfig directly in Config would have silently failed to decode
// every field, or failed the whole Collector config with a confusing
// "unused key" error. The generic-map-then-decodeStorage indirection is
// what avoids that, and this test exercises the real boundary rather than
// assuming it.
func TestConfigUnmarshalDecodesStorageBlock(t *testing.T) {
	conf := confmap.NewFromStringMap(map[string]any{
		"storage": map[string]any{
			"version": "v1",
			"type":    "postgres",
			"postgres": map[string]any{
				"dsn":                     "postgres://u:p@db:5432/trustvian?sslmode=disable",
				"max_connections":         10,
				"connect_timeout_seconds": 15,
			},
		},
	})

	var cfg trustvianprocessor.Config
	if err := conf.Unmarshal(&cfg); err != nil {
		t.Fatalf("conf.Unmarshal() error = %v", err)
	}
	if cfg.Storage == nil {
		t.Fatal("cfg.Storage = nil, want the decoded storage block")
	}
	if got := cfg.Storage["type"]; got != "postgres" {
		t.Errorf("cfg.Storage[%q] = %v, want %q", "type", got, "postgres")
	}
	pg, ok := cfg.Storage["postgres"].(map[string]any)
	if !ok {
		t.Fatalf("cfg.Storage[\"postgres\"] = %T, want a nested map", cfg.Storage["postgres"])
	}
	if got := pg["max_connections"]; got != 10 {
		t.Errorf("postgres.max_connections = %v, want 10 — nested snake_case keys must survive the decoder", got)
	}
}

// TestConfigUnmarshalOmittedStorageLeavesNilMap pins the distinction
// newTrustvianProcessor relies on: no `storage:` key at all leaves the
// field nil, which means "keep NewEngine's in-memory default" and
// preserves pre-037 behavior exactly. An explicitly present block — even
// an empty one — is an explicit request that must be validated.
func TestConfigUnmarshalOmittedStorageLeavesNilMap(t *testing.T) {
	var cfg trustvianprocessor.Config
	if err := confmap.NewFromStringMap(map[string]any{}).Unmarshal(&cfg); err != nil {
		t.Fatalf("conf.Unmarshal() error = %v", err)
	}
	if cfg.Storage != nil {
		t.Errorf("cfg.Storage = %v, want nil when no storage block is present", cfg.Storage)
	}
}

// newProcessor builds a processor through the real factory path, which is
// what a Collector does during pipeline graph construction — before
// Start, so a failure here fails Collector startup rather than surfacing
// at the first span.
func newProcessor(t *testing.T, cfg *trustvianprocessor.Config) (processor.Traces, error) {
	t.Helper()

	factory := trustvianprocessor.NewFactory()
	return factory.CreateTraces(
		context.Background(),
		processor.Settings{
			// The ID's type must match the registered component type, or
			// Collector rejects the call before the config is even read.
			ID:                component.NewID(component.MustNewType("trustvian")),
			TelemetrySettings: componenttest.NewNopTelemetrySettings(),
			BuildInfo:         component.NewDefaultBuildInfo(),
		},
		cfg,
		consumertest.NewNop(),
	)
}

// TestStorageMemoryBackendStarts confirms the canonical path works for the
// simplest backend, with no database involved: `type: memory` compiles and
// the processor starts. This is the control — without it, a failing
// PostgreSQL test could not be distinguished from storage configuration
// being broken outright.
func TestStorageMemoryBackendStarts(t *testing.T) {
	p, err := newProcessor(t, &trustvianprocessor.Config{
		Storage: map[string]any{"version": "v1", "type": "memory"},
	})
	if err != nil {
		t.Fatalf("CreateTraces() error = %v, want nil", err)
	}
	if p == nil {
		t.Fatal("CreateTraces() returned a nil processor with a nil error")
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestStorageFailsClosedOnUnreachablePostgres is the invariant the
// reference deployment depends on most, asserted at the Collector's own
// construction boundary: a processor explicitly configured for PostgreSQL
// that cannot reach it must **fail**, never start on the in-memory
// default.
//
// A Collector that fell back would keep scoring spans and enriching them
// with trust decisions derived from state that silently evaporates when
// the process exits — and the operator would have no signal that the
// database they configured was never used. That is the silent persistence
// downgrade docs/SECURITY.md prohibits, and containerization must not
// reintroduce it.
//
// Port 1 on loopback is never a PostgreSQL server, so this needs no
// database and runs anywhere.
func TestStorageFailsClosedOnUnreachablePostgres(t *testing.T) {
	p, err := newProcessor(t, &trustvianprocessor.Config{
		Storage: map[string]any{
			"version": "v1",
			"type":    "postgres",
			"postgres": map[string]any{
				"dsn":                     "postgres://trustvian:secret@127.0.0.1:1/trustvian?sslmode=disable",
				"connect_timeout_seconds": 2,
			},
		},
	})

	if err == nil {
		if p != nil {
			_ = p.Shutdown(context.Background())
		}
		t.Fatal("CreateTraces() error = nil for an unreachable database, want an error — the Collector must refuse to start")
	}
	if p != nil {
		t.Error("CreateTraces() returned a non-nil processor alongside a storage error — a Collector must never run on a substituted store")
	}
	// The DSN carries a password, and a Collector startup error goes
	// straight to its logs.
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("CreateTraces() err leaked DSN credentials: %v", err)
	}
}

// TestStorageFailsClosedOnInvalidConfig covers the other half: a
// well-formed-looking block that cannot be honored is rejected at startup
// rather than at the first span.
func TestStorageFailsClosedOnInvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		storage map[string]any
	}{
		{"unknown backend type", map[string]any{"version": "v1", "type": "mysql"}},
		{"postgres without a dsn", map[string]any{"version": "v1", "type": "postgres"}},
		{"unsupported schema version", map[string]any{"version": "v99", "type": "memory"}},
		{"empty block", map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := newProcessor(t, &trustvianprocessor.Config{Storage: tt.storage})
			if err == nil {
				if p != nil {
					_ = p.Shutdown(context.Background())
				}
				t.Fatal("CreateTraces() error = nil, want an error")
			}
			if p != nil {
				t.Error("CreateTraces() returned a non-nil processor alongside a storage error")
			}
		})
	}
}

// TestStorageOmittedPreservesInMemoryDefault is the backward-compatibility
// guarantee: a Collector config written before this field existed must
// behave exactly as it did. Construction succeeds with no storage block,
// and no database is contacted.
func TestStorageOmittedPreservesInMemoryDefault(t *testing.T) {
	p, err := newProcessor(t, &trustvianprocessor.Config{})
	if err != nil {
		t.Fatalf("CreateTraces() error = %v with no storage block, want nil", err)
	}
	if p == nil {
		t.Fatal("CreateTraces() returned a nil processor with a nil error")
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestStoragePostgresBackendStartsAndShutsDown is the one test here that
// needs a real database. It proves the full canonical chain resolves from
// a Collector config map all the way to a live PostgreSQL store —
// Config.Storage → decodeStorage → config.StorageConfig →
// config.CompileStorage → trustvian.WithStore — and that Shutdown
// releases the connection pool rather than leaving it to a server-side
// timeout.
//
// Repeating the construct/shutdown cycle is the point of the loop: if
// Shutdown did not actually close the pool, a Collector restart loop would
// accumulate server connections until PostgreSQL refused new ones.
func TestStoragePostgresBackendStartsAndShutsDown(t *testing.T) {
	dsn := os.Getenv("TRUSTVIAN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TRUSTVIAN_TEST_POSTGRES_DSN not set; see docs/storage-guide.md")
	}

	for range 3 {
		p, err := newProcessor(t, &trustvianprocessor.Config{
			Storage: map[string]any{
				"version": "v1",
				"type":    "postgres",
				"postgres": map[string]any{
					"dsn":             dsn,
					"max_connections": 2,
				},
			},
		})
		if err != nil {
			t.Fatalf("CreateTraces() error = %v, want nil against a reachable database", err)
		}
		if p == nil {
			t.Fatal("CreateTraces() returned a nil processor with a nil error")
		}
		if err := p.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown() error = %v", err)
		}
	}
}
