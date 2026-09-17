package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trustvian/trustvian/config"
)

func memoryStorageConfig() config.StorageConfig {
	return config.StorageConfig{
		Version: config.StorageSchemaVersionV1,
		Type:    config.StorageTypeMemory,
	}
}

func fileStorageConfig(path string) config.StorageConfig {
	return config.StorageConfig{
		Version: config.StorageSchemaVersionV1,
		Type:    config.StorageTypeFile,
		File:    &config.FileStorageConfig{Path: path},
	}
}

func postgresStorageConfig(dsn string) config.StorageConfig {
	return config.StorageConfig{
		Version:  config.StorageSchemaVersionV1,
		Type:     config.StorageTypePostgres,
		Postgres: &config.PostgresStorageConfig{DSN: dsn},
	}
}

func TestValidateMemoryStorageConfig(t *testing.T) {
	if err := memoryStorageConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateFileStorageConfig(t *testing.T) {
	if err := fileStorageConfig("/tmp/baseline.json").Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateStorageConfigRejectsUnsupportedVersion(t *testing.T) {
	cfg := memoryStorageConfig()
	cfg.Version = "v2"

	err := cfg.Validate()
	if !errors.Is(err, config.ErrUnsupportedStorageVersion) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrUnsupportedStorageVersion)
	}
}

func TestValidateStorageConfigRejectsMissingType(t *testing.T) {
	// An omitted Type is a validation error, never an implicit fallback
	// to the non-durable memory store — see StorageConfig's doc comment.
	cfg := memoryStorageConfig()
	cfg.Type = ""

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidStorageType) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidStorageType)
	}
}

func TestValidateStorageConfigRejectsUnknownType(t *testing.T) {
	cfg := memoryStorageConfig()
	cfg.Type = "postgersql" // typo

	err := cfg.Validate()
	if !errors.Is(err, config.ErrInvalidStorageType) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidStorageType)
	}
}

func TestValidateStorageConfigRejectsFileTypeWithoutPath(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StorageConfig
	}{
		{"nil file block", config.StorageConfig{Version: config.StorageSchemaVersionV1, Type: config.StorageTypeFile}},
		{"empty path", fileStorageConfig("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if !errors.Is(err, config.ErrMissingStoragePath) {
				t.Errorf("Validate() err = %v, want %v", err, config.ErrMissingStoragePath)
			}
		})
	}
}

// TestValidateStorageConfigAcceptsPostgresType pins the deliberate split
// between validation and construction: Validate checks that the document
// is *well-formed* — a DSN is present — and stops there. It deliberately
// does not parse the DSN's syntax or attempt a connection, which keeps
// Validate pure (no I/O, no network) and, just as importantly, keeps it
// from ever having to quote the DSN back in an error message. Reachability
// is CompileStorage's problem; see
// TestCompileStoragePostgresFailsClosedNeverFallsBack.
func TestValidateStorageConfigAcceptsPostgresType(t *testing.T) {
	cfg := postgresStorageConfig("postgres://u:p@localhost:5432/db")

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestValidateStorageConfigRejectsPostgresTypeWithoutDSN is the postgres
// analogue of TestValidateStorageConfigRejectsFileTypeWithoutPath: the
// backend block a Type requires must actually be present. Rejected at
// validation time, before any connection is attempted, so an operator
// finds out from reading their config rather than from a network error.
func TestValidateStorageConfigRejectsPostgresTypeWithoutDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StorageConfig
	}{
		{"no postgres block", func() config.StorageConfig {
			c := memoryStorageConfig()
			c.Type = config.StorageTypePostgres
			return c
		}()},
		{"empty dsn", postgresStorageConfig("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); !errors.Is(err, config.ErrMissingStorageDSN) {
				t.Errorf("Validate() = %v, want %v", err, config.ErrMissingStorageDSN)
			}
		})
	}
}

// TestValidateStorageConfigRejectsNegativePostgresTuning pins that the
// numeric knobs cannot be configured into nonsense. Zero means "use the
// default" for both, which is why only negative values are rejected.
func TestValidateStorageConfigRejectsNegativePostgresTuning(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*config.PostgresStorageConfig)
	}{
		{"negative max_connections", func(p *config.PostgresStorageConfig) { p.MaxConnections = -1 }},
		{"negative connect_timeout_seconds", func(p *config.PostgresStorageConfig) { p.ConnectTimeoutSeconds = -1 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := postgresStorageConfig("postgres://u:p@localhost:5432/db")
			tt.apply(cfg.Postgres)

			if err := cfg.Validate(); err == nil {
				t.Error("Validate() = nil, want an error")
			}
		})
	}
}

// TestValidateStorageConfigIgnoresUnusedBackendBlock pins that carrying
// a `file:` block while running `type: memory` is valid — so an operator
// can switch backends by editing one line.
func TestValidateStorageConfigIgnoresUnusedBackendBlock(t *testing.T) {
	cfg := memoryStorageConfig()
	cfg.File = &config.FileStorageConfig{Path: "/tmp/unused.json"}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestCompileStorageMemory(t *testing.T) {
	s, err := config.CompileStorage(memoryStorageConfig())
	if err != nil {
		t.Fatalf("CompileStorage() error = %v", err)
	}
	if s == nil {
		t.Fatal("CompileStorage() returned a nil Store with a nil error")
	}
}

func TestCompileStorageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")

	s, err := config.CompileStorage(fileStorageConfig(path))
	if err != nil {
		t.Fatalf("CompileStorage() error = %v", err)
	}
	if s == nil {
		t.Fatal("CompileStorage() returned a nil Store with a nil error")
	}
}

// TestCompileStoragePostgresFailsClosedNeverFallsBack is this milestone's
// central operations invariant: an explicit request for a durable backend
// that cannot be built must fail, never silently downgrade. Silently
// substituting memory or file storage would lose exactly the state the
// operator asked to persist — a data-loss bug disguised as convenience.
//
// Through task 034 this was proven with a backend that was recognized but
// unimplemented. Task 035 implemented it, so the proof moved to the
// failure that persists in production: the database is configured but
// unreachable. Port 1 on loopback is never a PostgreSQL server, so this
// needs no database and runs everywhere.
func TestCompileStoragePostgresFailsClosedNeverFallsBack(t *testing.T) {
	cfg := postgresStorageConfig("postgres://u:p@127.0.0.1:1/db?sslmode=disable")
	cfg.Postgres.ConnectTimeoutSeconds = 2

	s, err := config.CompileStorage(cfg)

	if err == nil {
		t.Fatal("CompileStorage() = nil error, want an error for an unreachable database")
	}
	if s != nil {
		t.Errorf("CompileStorage() returned a non-nil Store (%T) alongside an error — an unreachable backend must never yield a usable substitute store", s)
	}
	// Distinct from a typo'd backend name, so an operator can tell a
	// connectivity failure from a configuration failure.
	if errors.Is(err, config.ErrInvalidStorageType) {
		t.Errorf("CompileStorage() err = %v, want it NOT to be ErrInvalidStorageType — postgres is recognized and implemented, just unreachable here", err)
	}
	// The DSN carries a password; a startup error is the most likely
	// place for it to reach a log.
	if strings.Contains(err.Error(), ":p@") || strings.Contains(err.Error(), "password") {
		t.Errorf("CompileStorage() err leaked DSN credentials: %v", err)
	}
}

func TestCompileStorageInvalidConfigReturnsNilStore(t *testing.T) {
	cfg := memoryStorageConfig()
	cfg.Version = "bogus"

	s, err := config.CompileStorage(cfg)
	if !errors.Is(err, config.ErrUnsupportedStorageVersion) {
		t.Errorf("CompileStorage() err = %v, want %v", err, config.ErrUnsupportedStorageVersion)
	}
	if s != nil {
		t.Errorf("CompileStorage() returned a non-nil Store alongside a validation error")
	}
}

// TestCompileStorageFilePropagatesLoadFailure pins that a corrupt or
// unreadable state file surfaces at Engine construction, not on the
// first Observe — and fails closed rather than starting empty and
// silently discarding the existing history.
func TestCompileStorageFilePropagatesLoadFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := config.CompileStorage(fileStorageConfig(path))
	if err == nil {
		t.Fatal("CompileStorage() error = nil for an unparseable state file, want an error")
	}
	if s != nil {
		t.Errorf("CompileStorage() returned a non-nil Store alongside a load error")
	}
}

func TestLoadStorageValidDocument(t *testing.T) {
	data := []byte(`
version: v1
type: file
file:
  path: /var/lib/trustvian/baseline.json
`)

	cfg, err := config.LoadStorage(data)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if cfg.Type != config.StorageTypeFile {
		t.Errorf("Type = %q, want %q", cfg.Type, config.StorageTypeFile)
	}
	if cfg.File == nil || cfg.File.Path != "/var/lib/trustvian/baseline.json" {
		t.Errorf("File = %+v, want path /var/lib/trustvian/baseline.json", cfg.File)
	}
}

func TestLoadStorageRejectsEmptyInput(t *testing.T) {
	_, err := config.LoadStorage(nil)
	if !errors.Is(err, config.ErrEmptyInput) {
		t.Errorf("LoadStorage() err = %v, want %v", err, config.ErrEmptyInput)
	}
}

func TestLoadStorageRejectsUnknownField(t *testing.T) {
	data := []byte("version: v1\ntype: memory\ntyp: file\n")

	_, err := config.LoadStorage(data)
	if err == nil {
		t.Fatal("LoadStorage() error = nil, want an error for the unrecognized field")
	}
}

func TestLoadStorageRejectsDuplicateYAMLKeys(t *testing.T) {
	data := []byte("version: v1\ntype: memory\ntype: file\n")

	_, err := config.LoadStorage(data)
	if err == nil {
		t.Fatal("LoadStorage() error = nil, want an error for the duplicate key")
	}
}

func TestLoadStorageRejectsInvalidConfig(t *testing.T) {
	data := []byte("version: v1\ntype: file\n") // file type without a path

	_, err := config.LoadStorage(data)
	if !errors.Is(err, config.ErrMissingStoragePath) {
		t.Errorf("LoadStorage() err = %v, want %v", err, config.ErrMissingStoragePath)
	}
}

func TestLoadStorageFileValidDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage.yaml")
	if err := os.WriteFile(path, []byte("version: v1\ntype: memory\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadStorageFile(path)
	if err != nil {
		t.Fatalf("LoadStorageFile: %v", err)
	}
	if cfg.Type != config.StorageTypeMemory {
		t.Errorf("Type = %q, want %q", cfg.Type, config.StorageTypeMemory)
	}
}

func TestLoadStorageFileMissingFile(t *testing.T) {
	_, err := config.LoadStorageFile("/nonexistent/storage.yaml")
	if err == nil {
		t.Fatal("LoadStorageFile() error = nil, want an error for a missing file")
	}
}

// FuzzLoadStorage mirrors every other loader's one required invariant:
// arbitrary bytes must never panic the loader.
func FuzzLoadStorage(f *testing.F) {
	f.Add([]byte("version: v1\ntype: memory\n"))
	f.Add([]byte("version: v1\ntype: file\nfile:\n  path: /tmp/x.json\n"))
	f.Add([]byte(""))
	f.Add([]byte("{{{{"))
	f.Add([]byte("version: &a [*a]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = config.LoadStorage(data)
	})
}

// TestLoadStoragePostgresDocument is task 035's configuration-surface
// proof: every PostgreSQL knob an operator can set is reachable from a
// YAML document through the public config package alone — no internal
// import, no Go struct literal. If a field is implemented but not
// spellable in YAML, it is not actually available to an OSS user, which
// is the release-completeness bar task 033 established.
func TestLoadStoragePostgresDocument(t *testing.T) {
	data := []byte(`
version: v1
type: postgres
postgres:
  dsn: postgres://trustvian:secret@db.internal:5432/trustvian?sslmode=require
  max_connections: 25
  connect_timeout_seconds: 15
`)

	cfg, err := config.LoadStorage(data)
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if cfg.Type != config.StorageTypePostgres {
		t.Errorf("Type = %q, want %q", cfg.Type, config.StorageTypePostgres)
	}
	if cfg.Postgres == nil {
		t.Fatal("Postgres block = nil, want it populated")
	}
	if got, want := cfg.Postgres.DSN, "postgres://trustvian:secret@db.internal:5432/trustvian?sslmode=require"; got != want {
		t.Errorf("DSN = %q, want %q", got, want)
	}
	if got := cfg.Postgres.MaxConnections; got != 25 {
		t.Errorf("MaxConnections = %d, want 25", got)
	}
	if got := cfg.Postgres.ConnectTimeoutSeconds; got != 15 {
		t.Errorf("ConnectTimeoutSeconds = %d, want 15", got)
	}
}

// TestLoadStoragePostgresMinimalDocument pins that the two tuning knobs
// are genuinely optional: a DSN alone is a complete, valid document, and
// the omitted fields stay at zero so postgres.NewStore applies its own
// defaults rather than a value this layer invented.
func TestLoadStoragePostgresMinimalDocument(t *testing.T) {
	cfg, err := config.LoadStorage([]byte("version: v1\ntype: postgres\npostgres:\n  dsn: postgres:///trustvian\n"))
	if err != nil {
		t.Fatalf("LoadStorage: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil — a DSN alone is a complete document", err)
	}
	if cfg.Postgres.MaxConnections != 0 || cfg.Postgres.ConnectTimeoutSeconds != 0 {
		t.Errorf("omitted knobs = (%d, %d), want (0, 0) so the store layer owns the defaults",
			cfg.Postgres.MaxConnections, cfg.Postgres.ConnectTimeoutSeconds)
	}
}
