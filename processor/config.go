package trustvianprocessor

import (
	"fmt"

	"github.com/go-viper/mapstructure/v2"

	"github.com/Trustvian/trustvian/config"
)

// Config is this processor's Collector configuration. Policy, when
// present, declares a real Trustvian Policy using the exact same
// schema the Go SDK (config.LoadFile) and the CLI
// (`trustvian analyze --config`) already consume — see
// docs/tasks/022-collector-config-integration.md in the core
// repository. Omitting it (the zero value, a nil map) preserves this
// processor's original default-Policy behavior — see
// newTrustvianProcessor.
//
// Policy is typed as a generic map, not config.PolicyConfig itself,
// deliberately: Collector's confmap decoder only reads `mapstructure`
// struct tags, matched case-sensitively (see
// go.opentelemetry.io/collector/confmap/internal.caseSensitiveMatchName),
// and config.PolicyConfig only carries the `yaml:"..."` tags task 020
// added for its own file loader — so embedding it directly here would
// silently fail to decode any of its snake_case fields (or, with
// Collector's ErrorUnused enabled, fail the whole config with a
// confusing "unused key" error). Deferring the actual PolicyConfig
// decode to decodePolicy (below), using go-viper/mapstructure/v2
// directly against config.PolicyConfig's existing yaml tags, reuses
// the one canonical config model exactly as written — no
// processor-specific PolicyConfig/PolicyRule/PolicyCondition model is
// introduced, and config.PolicyConfig itself is not modified.
type Config struct {
	Policy map[string]any `mapstructure:"policy,omitempty"`
}

// decodePolicy converts the generic map Collector's decoder produced
// for the `policy:` block into a real config.PolicyConfig, using
// config.PolicyConfig's own `yaml:"..."` struct tags as the field
// mapping (go-viper/mapstructure/v2 accepts any tag name via
// DecoderConfig.TagName; "yaml" happens to be exactly the tag task 020
// already put there for its own loader).
//
// This performs no validation, no defaulting, and no policy
// evaluation of its own — it is a pure structural decode step. The one
// canonical config.CompilePolicy (called by the caller of this
// function) remains the sole place PolicyConfig is validated and
// compiled into a policy.Policy, exactly as it is for the Go SDK and
// the CLI.
func decodePolicy(raw map[string]any) (config.PolicyConfig, error) {
	var cfg config.PolicyConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "yaml",
		Result:  &cfg,
	})
	if err != nil {
		return config.PolicyConfig{}, fmt.Errorf("trustvianprocessor: policy decoder: %w", err)
	}
	if err := decoder.Decode(raw); err != nil {
		return config.PolicyConfig{}, fmt.Errorf("trustvianprocessor: policy: %w", err)
	}
	return cfg, nil
}
