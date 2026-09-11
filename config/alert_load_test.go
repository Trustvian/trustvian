package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Trustvian/trustvian/config"
)

func TestLoadAlertsValidDocument(t *testing.T) {
	data := []byte(`
version: v1
rules:
  - name: critical-risk
    when:
      min_risk_level: critical
    severity: critical
  - name: elevated-anomaly
    when:
      min_anomaly_score: 0.8
    severity: medium
`)

	cfg, err := config.LoadAlerts(data)
	if err != nil {
		t.Fatalf("LoadAlerts: %v", err)
	}
	if cfg.Version != config.AlertSchemaVersionV1 {
		t.Errorf("Version = %q, want %q", cfg.Version, config.AlertSchemaVersionV1)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("len(Rules) = %d, want 2", len(cfg.Rules))
	}
	if cfg.Rules[0].Name != "critical-risk" || cfg.Rules[1].Name != "elevated-anomaly" {
		t.Errorf("rule order not preserved: %+v", cfg.Rules)
	}
}

func TestLoadAlertsRejectsEmptyInput(t *testing.T) {
	_, err := config.LoadAlerts(nil)
	if !errors.Is(err, config.ErrEmptyInput) {
		t.Errorf("LoadAlerts() err = %v, want %v", err, config.ErrEmptyInput)
	}
}

func TestLoadAlertsRejectsUnknownField(t *testing.T) {
	data := []byte(`
version: v1
rules:
  - name: r1
    severty: high
`)
	_, err := config.LoadAlerts(data)
	if err == nil {
		t.Fatal("LoadAlerts() error = nil, want an error for the unrecognized field")
	}
}

func TestLoadAlertsRejectsDuplicateYAMLKeys(t *testing.T) {
	data := []byte(`
version: v1
rules:
  - name: r1
    severity: high
    severity: low
`)
	_, err := config.LoadAlerts(data)
	if err == nil {
		t.Fatal("LoadAlerts() error = nil, want an error for the duplicate key")
	}
}

func TestLoadAlertsRejectsInvalidConfig(t *testing.T) {
	data := []byte(`
version: v1
rules:
  - name: r1
    severity: urgent
`)
	_, err := config.LoadAlerts(data)
	if !errors.Is(err, config.ErrInvalidSeverity) {
		t.Errorf("LoadAlerts() err = %v, want %v", err, config.ErrInvalidSeverity)
	}
}

func TestLoadAlertsFileValidDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")
	data := []byte("version: v1\nrules:\n  - name: r1\n    severity: high\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.LoadAlertsFile(path)
	if err != nil {
		t.Fatalf("LoadAlertsFile: %v", err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Name != "r1" {
		t.Errorf("cfg = %+v, want one rule named r1", cfg)
	}
}

func TestLoadAlertsFileMissingFile(t *testing.T) {
	_, err := config.LoadAlertsFile("/nonexistent/alerts.yaml")
	if err == nil {
		t.Fatal("LoadAlertsFile() error = nil, want an error for a missing file")
	}
}

// FuzzLoadAlerts mirrors FuzzLoad's one required invariant for the
// AlertConfig loader: arbitrary bytes must never panic LoadAlerts.
func FuzzLoadAlerts(f *testing.F) {
	f.Add([]byte("version: v1\n"))
	f.Add([]byte("version: v1\nrules:\n  - name: r1\n    severity: high\n"))
	f.Add([]byte(""))
	f.Add([]byte("{{{{"))
	f.Add([]byte("version: &a [*a]"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = config.LoadAlerts(data)
	})
}

func TestLoadAlertsFileRejectsOversizedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.yaml")
	big := make([]byte, (1<<20)+1)
	for i := range big {
		big[i] = ' '
	}
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := config.LoadAlertsFile(path)
	if !errors.Is(err, config.ErrFileTooLarge) {
		t.Errorf("LoadAlertsFile() err = %v, want %v", err, config.ErrFileTooLarge)
	}
}
