package config_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Trustvian/trustvian/config"
	"github.com/Trustvian/trustvian/internal/anomaly"
)

func TestCompileAnomalyRejectsInvalidConfig(t *testing.T) {
	cfg := minimalValidAnomalyConfig()
	cfg.Version = "bogus"

	_, err := config.CompileAnomaly(cfg)
	if !errors.Is(err, config.ErrUnsupportedAnomalyVersion) {
		t.Errorf("CompileAnomaly() err = %v, want %v", err, config.ErrUnsupportedAnomalyVersion)
	}
}

// TestCompileAnomalyZeroValueMatchesDefaultConfig is this task's own
// mandatory backward-compatibility proof (§8 of the task brief):
// CompileAnomaly(AnomalyConfig{Version: "v1"}) — every pointer nil,
// every plain weight at its Go zero value — must reproduce
// anomaly.DefaultConfig() byte-for-byte. This is what makes "existing
// users who do nothing observe the same behavior as before" a proven
// fact, not an intention.
func TestCompileAnomalyZeroValueMatchesDefaultConfig(t *testing.T) {
	got, err := config.CompileAnomaly(minimalValidAnomalyConfig())
	if err != nil {
		t.Fatalf("CompileAnomaly: %v", err)
	}

	want := anomaly.DefaultConfig()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CompileAnomaly(zero-value config) = %+v,\nwant anomaly.DefaultConfig() = %+v", got, want)
	}
}

func TestCompileAnomalyIsDeterministic(t *testing.T) {
	cfg := fullValidAnomalyConfig()

	c1, err := config.CompileAnomaly(cfg)
	if err != nil {
		t.Fatalf("CompileAnomaly (1st): %v", err)
	}
	c2, err := config.CompileAnomaly(cfg)
	if err != nil {
		t.Fatalf("CompileAnomaly (2nd): %v", err)
	}

	if !reflect.DeepEqual(c1, c2) {
		t.Errorf("compiling the same AnomalyConfig twice produced different results:\n%+v\nvs\n%+v", c1, c2)
	}
}

func TestCompileAnomalyDoesNotMutateConfig(t *testing.T) {
	cfg := fullValidAnomalyConfig()
	before := fullValidAnomalyConfig()

	_, err := config.CompileAnomaly(cfg)
	if err != nil {
		t.Fatalf("CompileAnomaly: %v", err)
	}

	if !reflect.DeepEqual(cfg, before) {
		t.Errorf("CompileAnomaly mutated its input:\ngot  %+v\nwant %+v", cfg, before)
	}
}

// TestCompileAnomalyTranslatesEveryField proves every public field
// reaches its documented runtime counterpart — not just that
// CompileAnomaly returns no error.
func TestCompileAnomalyTranslatesEveryField(t *testing.T) {
	cfg := fullValidAnomalyConfig()

	got, err := config.CompileAnomaly(cfg)
	if err != nil {
		t.Fatalf("CompileAnomaly: %v", err)
	}

	want := anomaly.Config{
		MinObservations:           *cfg.MinObservations,
		LatencyZThreshold:         *cfg.LatencyZThreshold,
		FrequencyZThreshold:       *cfg.FrequencyZThreshold,
		NoveltyWeight:             *cfg.NoveltyWeight,
		LatencyWeight:             *cfg.LatencyWeight,
		ErrorWeight:               *cfg.ErrorWeight,
		FrequencyWeight:           cfg.FrequencyWeight,
		TimePatternWeight:         cfg.TimePatternWeight,
		TransitionWeight:          cfg.TransitionWeight,
		MinTransitionObservations: *cfg.MinTransitionObservations,
		TransitionRarityWeight:    cfg.TransitionRarityWeight,
		NGramWeight:               cfg.NGramWeight,
		MinNGramObservations:      *cfg.MinNGramObservations,
		NGramRarityWeight:         cfg.NGramRarityWeight,
		MarkovWeight:              cfg.MarkovWeight,
		DelegationWeight:          cfg.DelegationWeight,
		SensitiveTargetFloor:      cfg.SensitiveTargetFloor,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("CompileAnomaly() = %+v,\nwant %+v", got, want)
	}
}

// TestCompileAnomalyPartialOverridePreservesOtherDefaults proves the
// pointer-vs-plain-value design actually works as documented: setting
// only one pointer field leaves every other field — including the
// other two pointer fields — at anomaly.DefaultConfig()'s own value.
func TestCompileAnomalyPartialOverridePreservesOtherDefaults(t *testing.T) {
	novelty := 0.5
	cfg := config.AnomalyConfig{
		Version:       config.AnomalySchemaVersionV1,
		NoveltyWeight: &novelty,
	}

	got, err := config.CompileAnomaly(cfg)
	if err != nil {
		t.Fatalf("CompileAnomaly: %v", err)
	}

	want := anomaly.DefaultConfig()
	want.NoveltyWeight = 0.5
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CompileAnomaly() = %+v,\nwant %+v (only NoveltyWeight overridden)", got, want)
	}
}
