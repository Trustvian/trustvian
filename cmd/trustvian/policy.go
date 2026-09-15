package main

import (
	"fmt"
	"io"

	trustvian "github.com/Trustvian/trustvian"
	"github.com/Trustvian/trustvian/config"
	"github.com/Trustvian/trustvian/internal/policy"
	"github.com/Trustvian/trustvian/internal/trust"
)

// defaultPolicy is the CLI's starter policy: block on high or critical
// risk, alert on medium, allow otherwise. It exists so `trustvian
// analyze` produces a differentiated, informative report out of the box.
// A real deployment embedding the Go SDK is expected to configure its
// own policy via trustvian.WithPolicy — this one is not meant to be a
// production default.
func defaultPolicy() policy.Policy {
	return policy.Policy{
		Rules: []policy.Rule{
			{
				Name:   "block-high-risk",
				When:   policy.Condition{MinRiskLevel: trust.RiskHigh},
				Action: policy.DecisionBlock,
				Reason: "trust score indicates high or critical risk",
			},
			{
				Name:   "alert-medium-risk",
				When:   policy.Condition{MinRiskLevel: trust.RiskMedium},
				Action: policy.DecisionAlert,
				Reason: "trust score indicates elevated risk",
			},
		},
		DefaultAction: policy.DecisionAllow,
		DefaultReason: "risk within tolerance",
	}
}

// newEngine constructs the Engine every subcommand analyzes/observes
// against. With no configPath, it keeps the CLI's existing built-in
// defaultPolicy unchanged — this is the hard compatibility requirement
// task 021 exists to preserve, not just an implementation detail. With
// no anomalyConfigPath, the Engine keeps anomaly.DefaultConfig()
// unchanged — the identical compatibility requirement, extended to
// task 033's own new flag: an operator who never passes
// --anomaly-config sees byte-for-byte the same behavior as before that
// flag existed. With no storageConfigPath, the Engine keeps
// NewEngine's own default in-memory store — the same requirement again,
// extended to task 034's flag.
//
// With configPath/anomalyConfigPath set, it loads and compiles a real
// Policy/AnomalyConfig from that file via the same public config
// package (config.LoadFile/config.LoadAnomalyFile,
// config.CompilePolicy/config.CompileAnomaly) any other caller outside
// this module would use — this function does not read
// internal/policy or internal/anomaly itself for that path, and does
// not re-implement any parsing or validation the config package
// already owns. A load or compile failure here is returned, not
// swallowed: the caller must fail the whole command rather than fall
// back to a default, per this task's central security invariant (an
// explicitly requested config that cannot be safely loaded and
// compiled must never result in analysis continuing under a different
// configuration).
//
// The returned cleanup function releases any OS resources the selected
// store holds — today that means the PostgreSQL backend's connection
// pool. It is never nil, so every caller can `defer cleanup()`
// unconditionally, and it is a no-op for the memory and file backends.
// Releasing is an optional, type-asserted capability rather than a
// method on store.Store itself; see config.CompileStorage's Lifecycle
// section for why, and docs/storage-guide.md § Lifecycle.
func newEngine(configPath, anomalyConfigPath, storageConfigPath string) (*trustvian.Engine, func(), error) {
	opts := []trustvian.Option{trustvian.WithPolicy(defaultPolicy())}
	cleanup := func() {}

	if configPath != "" {
		cfg, err := config.LoadFile(configPath)
		if err != nil {
			return nil, cleanup, err
		}
		p, err := config.CompilePolicy(cfg)
		if err != nil {
			return nil, cleanup, fmt.Errorf("config %s: %w", configPath, err)
		}
		opts[0] = trustvian.WithPolicy(p)
	}

	if anomalyConfigPath != "" {
		acfg, err := config.LoadAnomalyFile(anomalyConfigPath)
		if err != nil {
			return nil, cleanup, err
		}
		ac, err := config.CompileAnomaly(acfg)
		if err != nil {
			return nil, cleanup, fmt.Errorf("anomaly-config %s: %w", anomalyConfigPath, err)
		}
		opts = append(opts, trustvian.WithAnomalyConfig(ac))
	}

	// Storage selection is the one option here with durability
	// consequences, so its failure handling matters most: any load or
	// compile error aborts, and the command must never fall back to the
	// default in-memory store after an explicit persistent-storage
	// request. Falling back would silently discard everything the run
	// learned — the exact data-loss-disguised-as-convenience failure
	// docs/adr/0018-production-store-boundary-and-postgresql-direction.md
	// rules out.
	if storageConfigPath != "" {
		scfg, err := config.LoadStorageFile(storageConfigPath)
		if err != nil {
			return nil, cleanup, err
		}
		s, err := config.CompileStorage(scfg)
		if err != nil {
			return nil, cleanup, fmt.Errorf("storage-config %s: %w", storageConfigPath, err)
		}
		if c, ok := s.(io.Closer); ok {
			cleanup = func() { _ = c.Close() }
		}
		opts = append(opts, trustvian.WithStore(s))
	}

	return trustvian.NewEngine(opts...), cleanup, nil
}
