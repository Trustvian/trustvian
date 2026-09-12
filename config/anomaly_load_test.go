package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Trustvian/trustvian/config"
)

func TestLoadAnomalyValidDocument(t *testing.T) {
	data := []byte(`
version: v1
delegation_weight: 0.7
ngram_weight: 0.7
sensitive_target_floor:
  secrets-manager: 0.8
`)

	cfg, err := config.LoadAnomaly(data)
	if err != nil {
		t.Fatalf("LoadAnomaly: %v", err)
	}
	if cfg.Version != config.AnomalySchemaVersionV1 {
		t.Errorf("Version = %q, want %q", cfg.Version, config.AnomalySchemaVersionV1)
	}
	if cfg.DelegationWeight != 0.7 {
		t.Errorf("DelegationWeight = %v, want 0.7", cfg.DelegationWeight)
	}
	if cfg.NGramWeight != 0.7 {
		t.Errorf("NGramWeight = %v, want 0.7", cfg.NGramWeight)
	}
	if cfg.SensitiveTargetFloor["secrets-manager"] != 0.8 {
		t.Errorf("SensitiveTargetFloor[secrets-manager] = %v, want 0.8", cfg.SensitiveTargetFloor["secrets-manager"])
	}
}

func TestLoadAnomalyValidDocumentWithPointerFields(t *testing.T) {
	data := []byte(`
version: v1
min_observations: 30
latency_z_threshold: 2.5
novelty_weight: 0.9
`)

	cfg, err := config.LoadAnomaly(data)
	if err != nil {
		t.Fatalf("LoadAnomaly: %v", err)
	}
	if cfg.MinObservations == nil || *cfg.MinObservations != 30 {
		t.Errorf("MinObservations = %v, want 30", cfg.MinObservations)
	}
	if cfg.LatencyZThreshold == nil || *cfg.LatencyZThreshold != 2.5 {
		t.Errorf("LatencyZThreshold = %v, want 2.5", cfg.LatencyZThreshold)
	}
	if cfg.NoveltyWeight == nil || *cfg.NoveltyWeight != 0.9 {
		t.Errorf("NoveltyWeight = %v, want 0.9", cfg.NoveltyWeight)
	}
}

func TestLoadAnomalyOmittedFieldsStayNil(t *testing.T) {
	cfg, err := config.LoadAnomaly([]byte("version: v1\n"))
	if err != nil {
		t.Fatalf("LoadAnomaly: %v", err)
	}
	if cfg.MinObservations != nil {
		t.Errorf("MinObservations = %v, want nil (omitted)", *cfg.MinObservations)
	}
	if cfg.NoveltyWeight != nil {
		t.Errorf("NoveltyWeight = %v, want nil (omitted)", *cfg.NoveltyWeight)
	}
	if cfg.DelegationWeight != 0 {
		t.Errorf("DelegationWeight = %v, want 0", cfg.DelegationWeight)
	}
}

func TestLoadAnomalyRejectsEmptyInput(t *testing.T) {
	_, err := config.LoadAnomaly(nil)
	if !errors.Is(err, config.ErrEmptyInput) {
		t.Errorf("LoadAnomaly() err = %v, want %v", err, config.ErrEmptyInput)
	}
}

func TestLoadAnomalyRejectsUnknownField(t *testing.T) {
	data := []byte(`
version: v1
delegaton_weight: 0.7
`)
	_, err := config.LoadAnomaly(data)
	if err == nil {
		t.Fatal("LoadAnomaly() error = nil, want an error for the unrecognized field")
	}
}

func TestLoadAnomalyRejectsDuplicateYAMLKeys(t *testing.T) {
	data := []byte(`
version: v1
delegation_weight: 0.7
delegation_weight: 0.3
`)
	_, err := config.LoadAnomaly(data)
	if err == nil {
		t.Fatal("LoadAnomaly() error = nil, want an error for the duplicate key")
	}
}

func TestLoadAnomalyRejectsInvalidConfig(t *testing.T) {
	data := []byte(`
version: v1
delegation_weight: 1.5
`)
	_, err := config.LoadAnomaly(data)
	if !errors.Is(err, config.ErrInvalidThreshold) {
		t.Errorf("LoadAnomaly() err = %v, want %v", err, config.ErrInvalidThreshold)
	}
}

// TestLoadAnomalyRejectsNegativeIntoUnsignedField is this task's
// mandatory "invalid state bounds" proof (§11/§24 of the task brief):
// a negative YAML literal into a uint64 field (MinObservations etc.)
// must be rejected by the decoder itself, never silently wrapped into
// a huge positive value or otherwise misinterpreted — this is checked
// empirically against the real decoder, not assumed of the library,
// the same discipline TestLoadRejectsDuplicateKeys already established
// for PolicyConfig.
func TestLoadAnomalyRejectsNegativeIntoUnsignedField(t *testing.T) {
	fields := []string{"min_observations", "min_transition_observations", "min_ngram_observations"}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			data := []byte("version: v1\n" + field + ": -1\n")
			_, err := config.LoadAnomaly(data)
			if err == nil {
				t.Fatalf("LoadAnomaly() error = nil for %s: -1, want an error — a negative literal must never silently become a huge uint64", field)
			}
		})
	}
}

func TestLoadAnomalyFileValidDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anomaly.yaml")
	data := []byte("version: v1\ndelegation_weight: 0.7\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadAnomalyFile(path)
	if err != nil {
		t.Fatalf("LoadAnomalyFile: %v", err)
	}
	if cfg.DelegationWeight != 0.7 {
		t.Errorf("DelegationWeight = %v, want 0.7", cfg.DelegationWeight)
	}
}

func TestLoadAnomalyFileMissingFile(t *testing.T) {
	_, err := config.LoadAnomalyFile("/nonexistent/anomaly.yaml")
	if err == nil {
		t.Fatal("LoadAnomalyFile() error = nil, want an error for a missing file")
	}
}

func TestLoadAnomalyFileRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.yaml")
	big := make([]byte, (1<<20)+1)
	for i := range big {
		big[i] = ' '
	}
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := config.LoadAnomalyFile(path)
	if !errors.Is(err, config.ErrFileTooLarge) {
		t.Errorf("LoadAnomalyFile() err = %v, want %v", err, config.ErrFileTooLarge)
	}
}

// FuzzLoadAnomaly mirrors FuzzLoadAlerts's one required invariant:
// arbitrary bytes must never panic LoadAnomaly.
func FuzzLoadAnomaly(f *testing.F) {
	f.Add([]byte("version: v1\n"))
	f.Add([]byte("version: v1\ndelegation_weight: 0.7\n"))
	f.Add([]byte(""))
	f.Add([]byte("{{{{"))
	f.Add([]byte("version: &a [*a]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = config.LoadAnomaly(data)
	})
}
