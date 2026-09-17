package trustvianprocessor

import (
	"fmt"
	"time"

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
	Health  *HealthConfig  `mapstructure:"health,omitempty"`
}

// HealthConfig enables the runtime's liveness and readiness endpoints.
//
// A pointer, so its absence is distinguishable from a zero value: omitting
// the `health:` block disables the listener entirely and leaves every
// pre-existing Collector config behaving exactly as before.
//
// Unlike Policy and Storage above, this decodes directly through
// `mapstructure` tags rather than via a generic map. There is no canonical
// Trustvian config type to reuse here — health is a property of *this*
// runtime, not of the engine — so there is nothing to defer to, and the
// indirection those two fields need would buy nothing.
type HealthConfig struct {
	// Endpoint is the address the health listener binds. Defaults to
	// defaultHealthEndpoint.
	//
	// Bind to a loopback or internal address in deployments where probes
	// come from the same host; the endpoints are unauthenticated by design
	// (see internal/health), so network placement is the access control.
	Endpoint string `mapstructure:"endpoint,omitempty"`

	// ReadinessTimeout bounds each readiness probe of the configured store.
	// Defaults to defaultReadinessTimeout.
	//
	// Exists because a wedged database must not hold /readyz open: without
	// a bound, the endpoint an operator uses to detect the problem becomes
	// the second thing the problem breaks.
	ReadinessTimeout time.Duration `mapstructure:"readiness_timeout,omitempty"`
}

const (
	// 13133 is the port the OpenTelemetry Collector ecosystem conventionally
	// uses for health endpoints, so it is the least surprising choice for an
	// operator already running Collectors.
	defaultHealthEndpoint = "0.0.0.0:13133"

	// Long enough to absorb a slow round trip to a healthy database, short
	// enough that a probe answers well within a typical supervisor interval.
	defaultReadinessTimeout = 2 * time.Second
)

// withDefaults returns the config with unset fields filled in. Returns a
// value rather than mutating, so the decoded config stays untouched.
func (h HealthConfig) withDefaults() HealthConfig {
	if h.Endpoint == "" {
		h.Endpoint = defaultHealthEndpoint
	}
	if h.ReadinessTimeout <= 0 {
		h.ReadinessTimeout = defaultReadinessTimeout
	}
	return h
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
