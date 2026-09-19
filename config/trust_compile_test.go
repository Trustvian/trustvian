package config_test

import (
	"errors"
	"testing"

	"github.com/trustvian/trustvian/config"
	"github.com/trustvian/trustvian/internal/trust"
)

func f64(v float64) *float64 { return &v }

// TestCompileTrustDefaultsAreUnchanged is the compatibility guarantee
// that matters most: a caller who names the document and sets nothing
// else gets exactly the built-in behavior, so adopting the public trust
// facade cannot move anyone's risk classification.
func TestCompileTrustDefaultsAreUnchanged(t *testing.T) {
	got, err := config.CompileTrust(config.TrustConfig{Version: config.TrustSchemaVersionV1})
	if err != nil {
		t.Fatalf("CompileTrust() error = %v", err)
	}
	if want := trust.DefaultConfig(); got != want {
		t.Fatalf("CompileTrust() = %+v, want %+v", got, want)
	}
}

// TestCompileTrustAppliesSetThresholds proves the public → internal
// translation carries each field to the field it names.
func TestCompileTrustAppliesSetThresholds(t *testing.T) {
	got, err := config.CompileTrust(config.TrustConfig{
		Version:           config.TrustSchemaVersionV1,
		MediumThreshold:   f64(0.1),
		HighThreshold:     f64(0.4),
		CriticalThreshold: f64(0.9),
	})
	if err != nil {
		t.Fatalf("CompileTrust() error = %v", err)
	}
	want := trust.Config{MediumThreshold: 0.1, HighThreshold: 0.4, CriticalThreshold: 0.9}
	if got != want {
		t.Fatalf("CompileTrust() = %+v, want %+v", got, want)
	}
}

// TestCompileTrustPartialOverrideKeepsOtherDefaults: a nil threshold is
// "use the default", not "use zero" — the distinction the pointer
// fields exist for.
func TestCompileTrustPartialOverrideKeepsOtherDefaults(t *testing.T) {
	got, err := config.CompileTrust(config.TrustConfig{
		Version:       config.TrustSchemaVersionV1,
		HighThreshold: f64(0.6),
	})
	if err != nil {
		t.Fatalf("CompileTrust() error = %v", err)
	}
	def := trust.DefaultConfig()
	if got.MediumThreshold != def.MediumThreshold || got.CriticalThreshold != def.CriticalThreshold {
		t.Fatalf("unset thresholds changed: got %+v, defaults %+v", got, def)
	}
	if got.HighThreshold != 0.6 {
		t.Fatalf("HighThreshold = %v, want 0.6", got.HighThreshold)
	}
}

func TestCompileTrustRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.TrustConfig
		want error
	}{
		{
			name: "missing version",
			cfg:  config.TrustConfig{},
			want: config.ErrUnsupportedTrustVersion,
		},
		{
			name: "unknown version",
			cfg:  config.TrustConfig{Version: "v99"},
			want: config.ErrUnsupportedTrustVersion,
		},
		{
			name: "threshold above one",
			cfg:  config.TrustConfig{Version: config.TrustSchemaVersionV1, HighThreshold: f64(1.5)},
			want: config.ErrInvalidThreshold,
		},
		{
			name: "negative threshold",
			cfg:  config.TrustConfig{Version: config.TrustSchemaVersionV1, MediumThreshold: f64(-0.1)},
			want: config.ErrInvalidThreshold,
		},
		{
			name: "equal thresholds leave a level unreachable",
			cfg: config.TrustConfig{
				Version:       config.TrustSchemaVersionV1,
				HighThreshold: f64(0.25), // equal to the default medium
			},
			want: config.ErrNonAscendingThresholds,
		},
		{
			name: "inverted against an unset neighbour",
			cfg: config.TrustConfig{
				Version:           config.TrustSchemaVersionV1,
				CriticalThreshold: f64(0.3), // below the default high of 0.5
			},
			want: config.ErrNonAscendingThresholds,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.CompileTrust(tt.cfg)
			if !errors.Is(err, tt.want) {
				t.Fatalf("CompileTrust() error = %v, want errors.Is(..., %v)", err, tt.want)
			}
		})
	}
}

// TestCompileTrustAcceptsBoundaryValues: 0 and 1 are legal thresholds
// individually — only the ordering between them constrains the ladder.
func TestCompileTrustAcceptsBoundaryValues(t *testing.T) {
	got, err := config.CompileTrust(config.TrustConfig{
		Version:           config.TrustSchemaVersionV1,
		MediumThreshold:   f64(0),
		HighThreshold:     f64(0.5),
		CriticalThreshold: f64(1),
	})
	if err != nil {
		t.Fatalf("CompileTrust() error = %v", err)
	}
	if got.MediumThreshold != 0 || got.CriticalThreshold != 1 {
		t.Fatalf("boundary values not carried through: %+v", got)
	}
}
