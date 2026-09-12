package config_test

import (
	"errors"
	"math"
	"testing"

	"github.com/Trustvian/trustvian/config"
)

func minimalValidAnomalyConfig() config.AnomalyConfig {
	return config.AnomalyConfig{
		Version: config.AnomalySchemaVersionV1,
	}
}

func fullValidAnomalyConfig() config.AnomalyConfig {
	minObs := uint64(30)
	latencyZ := 2.5
	frequencyZ := 4.0
	novelty := 0.9
	latency := 0.5
	errWeight := 0.7
	minTransition := uint64(15)
	minNGram := uint64(15)
	return config.AnomalyConfig{
		Version:                   config.AnomalySchemaVersionV1,
		MinObservations:           &minObs,
		LatencyZThreshold:         &latencyZ,
		FrequencyZThreshold:       &frequencyZ,
		NoveltyWeight:             &novelty,
		LatencyWeight:             &latency,
		ErrorWeight:               &errWeight,
		FrequencyWeight:           0.3,
		TimePatternWeight:         0.2,
		TransitionWeight:          0.7,
		MinTransitionObservations: &minTransition,
		TransitionRarityWeight:    0.6,
		NGramWeight:               0.7,
		MinNGramObservations:      &minNGram,
		NGramRarityWeight:         0.6,
		MarkovWeight:              0.5,
		DelegationWeight:          0.7,
		SensitiveTargetFloor:      map[string]float64{"secrets-manager": 0.8},
	}
}

func TestValidateMinimalAnomalyConfig(t *testing.T) {
	if err := minimalValidAnomalyConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateFullAnomalyConfig(t *testing.T) {
	if err := fullValidAnomalyConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidateAnomalyConfigRejectsUnsupportedVersion(t *testing.T) {
	cfg := minimalValidAnomalyConfig()
	cfg.Version = "v2"

	err := cfg.Validate()
	if !errors.Is(err, config.ErrUnsupportedAnomalyVersion) {
		t.Errorf("Validate() err = %v, want %v", err, config.ErrUnsupportedAnomalyVersion)
	}
}

// TestValidateAnomalyConfigRejectsInvalidWeight covers every plain
// (non-pointer) weight field with the same negative/above-one/NaN/Inf
// table TestValidateAlertConfigRejectsInvalidMinAnomalyScore already
// establishes for AlertConditionConfig.MinAnomalyScore.
func TestValidateAnomalyConfigRejectsInvalidWeight(t *testing.T) {
	badValues := []struct {
		name  string
		value float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	}

	fields := []struct {
		name  string
		apply func(cfg *config.AnomalyConfig, v float64)
	}{
		{"frequency_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.FrequencyWeight = v }},
		{"time_pattern_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.TimePatternWeight = v }},
		{"transition_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.TransitionWeight = v }},
		{"transition_rarity_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.TransitionRarityWeight = v }},
		{"ngram_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.NGramWeight = v }},
		{"ngram_rarity_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.NGramRarityWeight = v }},
		{"markov_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.MarkovWeight = v }},
		{"delegation_weight", func(cfg *config.AnomalyConfig, v float64) { cfg.DelegationWeight = v }},
	}

	for _, field := range fields {
		for _, bad := range badValues {
			t.Run(field.name+"/"+bad.name, func(t *testing.T) {
				cfg := minimalValidAnomalyConfig()
				field.apply(&cfg, bad.value)

				err := cfg.Validate()
				if !errors.Is(err, config.ErrInvalidThreshold) {
					t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidThreshold)
				}
			})
		}
	}
}

// TestValidateAnomalyConfigRejectsInvalidPointerWeight covers the
// pointer weight fields (NoveltyWeight/LatencyWeight/ErrorWeight) —
// same range, different zero-value semantics (see AnomalyConfig's own
// doc comment for why these three are pointers).
func TestValidateAnomalyConfigRejectsInvalidPointerWeight(t *testing.T) {
	bad := 2.0

	tests := []struct {
		name  string
		apply func(cfg *config.AnomalyConfig)
	}{
		{"novelty_weight", func(cfg *config.AnomalyConfig) { cfg.NoveltyWeight = &bad }},
		{"latency_weight", func(cfg *config.AnomalyConfig) { cfg.LatencyWeight = &bad }},
		{"error_weight", func(cfg *config.AnomalyConfig) { cfg.ErrorWeight = &bad }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := minimalValidAnomalyConfig()
			tt.apply(&cfg)

			err := cfg.Validate()
			if !errors.Is(err, config.ErrInvalidThreshold) {
				t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidThreshold)
			}
		})
	}
}

func TestValidateAnomalyConfigRejectsInvalidZThreshold(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"zero", 0},
		{"negative", -1.0},
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	}
	for _, tt := range tests {
		t.Run("latency/"+tt.name, func(t *testing.T) {
			v := tt.value
			cfg := minimalValidAnomalyConfig()
			cfg.LatencyZThreshold = &v

			err := cfg.Validate()
			if !errors.Is(err, config.ErrInvalidZThreshold) {
				t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidZThreshold)
			}
		})
		t.Run("frequency/"+tt.name, func(t *testing.T) {
			v := tt.value
			cfg := minimalValidAnomalyConfig()
			cfg.FrequencyZThreshold = &v

			err := cfg.Validate()
			if !errors.Is(err, config.ErrInvalidZThreshold) {
				t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidZThreshold)
			}
		})
	}
}

func TestValidateAnomalyConfigAcceptsZeroMinObservations(t *testing.T) {
	// 0 is a genuine, internally-handled configuration ("everything is
	// immediately mature"), not an error — see anomaly.Score's own
	// familiarity computation.
	zero := uint64(0)
	cfg := minimalValidAnomalyConfig()
	cfg.MinObservations = &zero
	cfg.MinTransitionObservations = &zero
	cfg.MinNGramObservations = &zero

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil — MinObservations=0 is a valid configuration", err)
	}
}

func TestValidateAnomalyConfigRejectsInvalidSensitiveTargetFloor(t *testing.T) {
	tests := []struct {
		name  string
		value float64
	}{
		{"negative", -0.1},
		{"above one", 1.1},
		{"NaN", math.NaN()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := minimalValidAnomalyConfig()
			cfg.SensitiveTargetFloor = map[string]float64{"secrets-manager": tt.value}

			err := cfg.Validate()
			if !errors.Is(err, config.ErrInvalidThreshold) {
				t.Errorf("Validate() err = %v, want %v", err, config.ErrInvalidThreshold)
			}
		})
	}
}

func TestValidateAnomalyConfigAcceptsEmptySensitiveTargetFloor(t *testing.T) {
	cfg := minimalValidAnomalyConfig()
	cfg.SensitiveTargetFloor = map[string]float64{}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}
