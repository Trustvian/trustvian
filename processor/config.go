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
// Storage, when present, selects the Store the Engine persists learned
// baselines into, using the same config.StorageConfig schema the Go SDK
// and `trustvian analyze --storage-config` already consume. Omitting it
// keeps NewEngine's own default in-memory store, so a Collector that
// never configured storage behaves exactly as it did before this field
// existed.
//
// This field is why a Collector deployment can persist at all. Before it,
// newTrustvianProcessor called NewEngine with at most WithPolicy and
// never WithStore, so every Collector ran on the non-durable default and
// discarded every baseline on restart — the same gap task 034 found in
// the CLI, in the one runtime that is actually long-lived.
//
// Typed as a generic map for the identical reason Policy is, and decoded
// by decodeStorage below into the real config.StorageConfig. See
// docs/tasks/037-reference-docker-compose-deployment.md.
type Config struct {
	Policy  map[string]any `mapstructure:"policy,omitempty"`
	Storage map[string]any `mapstructure:"storage,omitempty"`
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

// decodeStorage is decodePolicy's exact counterpart for the `storage:`
// block: a pure structural decode of Collector's generic map into the
// canonical config.StorageConfig, using that type's own `yaml:"..."` tags
// as the field mapping.
//
// It performs no validation, no defaulting, and — critically — no
// database construction of its own. config.CompileStorage remains the
// sole place a StorageConfig is validated and turned into a Store, so
// this processor inherits every guarantee that function carries: the
// fail-closed contract (a nil Store on any error, never a silent
// downgrade to non-durable storage), the credential handling (the DSN is
// never logged or wrapped into an error), and the schema-compatibility
// checks. A processor-side pgx connection or DSN parser would have
// forfeited all of it — see
// docs/adr/0018-production-store-boundary-and-postgresql-direction.md.
func decodeStorage(raw map[string]any) (config.StorageConfig, error) {
	var cfg config.StorageConfig
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		TagName: "yaml",
		Result:  &cfg,
	})
	if err != nil {
		return config.StorageConfig{}, fmt.Errorf("trustvianprocessor: storage decoder: %w", err)
	}
	if err := decoder.Decode(raw); err != nil {
		return config.StorageConfig{}, fmt.Errorf("trustvianprocessor: storage: %w", err)
	}
	return cfg, nil
}
