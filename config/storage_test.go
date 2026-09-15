package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Trustvian/trustvian/config"
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
// between validation and construction: `type: postgres` is *well-formed*
// configuration, so Validate accepts it; only CompileStorage reports
// that this release cannot build it. That split is what lets a future
// release implement PostgreSQL by changing CompileStorage alone, with no
// validation change and no config that previously validated suddenly
// failing.
func TestValidateStorageConfigAcceptsPostgresType(t *testing.T) {
	cfg := memoryStorageConfig()
	cfg.Type = config.StorageTypePostgres

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil — postgres is well-formed config, rejected at compile time, not validation time", err)
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

// TestCompileStoragePostgresFailsClosedNeverFallsBack is this task's
// central operations invariant: an explicit request for a durable
// backend this release cannot build must fail, never silently downgrade.
// Silently substituting memory or file storage would lose exactly the
// state the operator asked to persist — a data-loss bug disguised as
// convenience.
func TestCompileStoragePostgresFailsClosedNeverFallsBack(t *testing.T) {
	cfg := memoryStorageConfig()
	cfg.Type = config.StorageTypePostgres

	s, err := config.CompileStorage(cfg)

	if !errors.Is(err, config.ErrStorageTypeNotImplemented) {
		t.Errorf("CompileStorage() err = %v, want %v", err, config.ErrStorageTypeNotImplemented)
	}
	if s != nil {
		t.Errorf("CompileStorage() returned a non-nil Store (%T) alongside an error — an unimplemented backend must never yield a usable substitute store", s)
	}
	// Distinct from a typo'd backend name, so an operator can tell the
	// two apart.
	if errors.Is(err, config.ErrInvalidStorageType) {
		t.Errorf("CompileStorage() err = %v, want it NOT to be ErrInvalidStorageType — postgres is recognized, just unimplemented", err)
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
