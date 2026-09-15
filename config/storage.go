// Storage configuration is a fourth independent document, alongside
// PolicyConfig, AlertConfig, and AnomalyConfig — for the identical
// reason those three are independent of each other (see
// docs/adr/0009-alert-config-is-a-separate-document.md): Policy answers
// "what decision," Alert answers "which Results notify," Anomaly
// answers "how is deviation scored," and this document answers a
// fourth, distinct question — "where does learned behavioral state
// live." All four compile independently
// (StorageConfig -> CompileStorage -> store.Store).
//
// See docs/adr/0018-production-store-boundary-and-postgresql-direction.md
// for why this document exists at all. In short: before it, an external
// consumer could not select *any* Store implementation — not even the
// fully-implemented, durable FileStore. store.Store's own methods
// reference internal types (baseline.Baseline, fingerprint.Fingerprint,
// features.VolatileFeatures), so an outside caller can neither
// implement the interface nor call store.NewInMemory/NewFileStore, and
// no exported function returned one for pass-through. Every external
// deployment was therefore silently pinned to the default in-memory
// store, losing all learned baselines on restart — the exact gap
// docs/ROADMAP.md § v0.8's own objective ("deployable as a real
// production system, not only a library and CLI against a local file")
// names. This is the same class of gap config.CompilePolicy closed for
// policy.Policy in v0.5 (ADR 0008) and config.CompileAnomaly closed for
// anomaly.Config in v0.7 (ADR 0017).

package config

// StorageSchemaVersionV1 is the only StorageConfig schema version this
// package currently accepts — independently versioned from
// SchemaVersionV1/AlertSchemaVersionV1/AnomalySchemaVersionV1, for the
// identical reason those are independent of each other (see
// SchemaVersionV1's own doc comment and ADR 0008 § Schema versioning).
const StorageSchemaVersionV1 = "v1"

// Storage type values accepted by StorageConfig.Type:
//
//   - StorageTypeMemory — ephemeral; nothing survives the process.
//     NewEngine's own default when WithStore is not passed at all.
//   - StorageTypeFile — local, single-process durable (store.FileStore).
//   - StorageTypePostgres — production, multi-process concurrent durable
//     (internal/store/postgres), implemented in `v0.8` task 035. The
//     config spelling was reserved by task 034, which recognized the
//     value and failed closed on it, so enabling it required no rename
//     and no migration for anyone who had already written it.
//
// Every backend fails *closed*: a store that cannot be constructed
// yields an error and a nil Store, never a silent downgrade to a
// different backend. Substituting a non-durable store for an explicitly
// requested durable one would lose exactly the learned state the
// operator asked to persist — see
// TestCompileStorageUnknownTypeFailsClosedNeverFallsBack and, for
// PostgreSQL specifically, TestCompileStoragePostgresUnreachableFailsClosed.
const (
	StorageTypeMemory   = "memory"
	StorageTypeFile     = "file"
	StorageTypePostgres = "postgres"
)

// StorageConfig is the public, declarative description of which
// store.Store implementation an Engine should use — compiled and
// validated by CompileStorage into the store.Store value
// trustvian.WithStore accepts.
//
// Unlike PolicyConfig/AlertConfig/AnomalyConfig, this document does not
// mirror a single internal type field-for-field: store.Store is an
// interface with several implementations, each taking different
// construction parameters, so this is a discriminated union keyed on
// Type rather than a flat field set. Only the backend named by Type has
// its options read; the others are ignored (and validated only if
// present), so a config file may carry a `file:` block while running
// `type: memory` without error — useful for switching backends by
// editing one line.
//
// There is deliberately no "default" Type: omitting it is a validation
// error, not an implicit fallback to memory. An operator writing a
// storage document at all has expressed intent to choose a backend;
// silently picking the non-durable one would be the same
// fail-open-on-ambiguous-config mistake policy.Policy.Evaluate's own
// fail-closed design exists to avoid. A caller who wants the in-memory
// default simply does not pass WithStore at all — NewEngine's own
// default is unchanged and remains store.NewInMemory().
type StorageConfig struct {
	// Version must be StorageSchemaVersionV1 today. Required, not
	// defaulted — same reasoning as every other config document's
	// Version field.
	Version string `yaml:"version"`

	// Type selects the backend: StorageTypeMemory, StorageTypeFile, or
	// StorageTypePostgres (recognized, not yet implemented — see the
	// constant block above). Required.
	Type string `yaml:"type"`

	// File carries StorageTypeFile's options. Required when Type is
	// "file", ignored otherwise.
	File *FileStorageConfig `yaml:"file,omitempty"`

	// Postgres carries StorageTypePostgres's options. Required when Type
	// is "postgres", ignored otherwise.
	Postgres *PostgresStorageConfig `yaml:"postgres,omitempty"`
}

// PostgresStorageConfig configures the PostgreSQL-backed store — the
// production-concurrency option, alongside `memory` (ephemeral) and
// `file` (local, single-process durable). Deliberately three fields:
// pgx exposes a large tuning surface, and each knob here had to justify
// itself as something an operator genuinely cannot set another way (see
// docs/adr/0018-production-store-boundary-and-postgresql-direction.md).
type PostgresStorageConfig struct {
	// DSN is the connection string, in URL
	// (`postgres://user:pw@host:5432/db`) or keyword/value
	// (`host=... user=...`) form. Required.
	//
	// **This value is a secret.** It typically embeds a password.
	// Nothing in this package or the store implementation logs it,
	// returns it inside an error, or writes it into persisted state —
	// including on a parse failure, which is the one case the driver's
	// own error message would otherwise echo it verbatim. See
	// docs/SECURITY.md § Storage configuration and production
	// persistence.
	DSN string `yaml:"dsn"`

	// MaxConnections caps the connection pool. Omitted or zero means the
	// driver default (the greater of 4 and GOMAXPROCS). Set it when the
	// deployment's own PostgreSQL connection limit, or the number of
	// Trustvian replicas sharing it, makes that default wrong in either
	// direction.
	MaxConnections int32 `yaml:"max_connections,omitempty"`

	// ConnectTimeoutSeconds bounds the startup connectivity check.
	// Omitted or zero means 10 seconds. This is what keeps fail-fast
	// startup *fast*: unbounded, an unreachable database would leave
	// construction waiting out the OS-level TCP timeout. Expressed in
	// whole seconds rather than a duration string to keep the YAML
	// schema free of format ambiguity, matching how every other numeric
	// field in this package is a plain number.
	ConnectTimeoutSeconds int `yaml:"connect_timeout_seconds,omitempty"`
}

// FileStorageConfig configures the JSON-file-backed store
// (store.NewFileStore). Path is the only option: FileStore itself has
// no tunable knobs — no flush interval, no buffering, no background
// goroutine (see its own doc comment and ADR 0006 for why that
// simplicity was a deliberate MVP choice, not an omission), so there is
// nothing else here to expose.
type FileStorageConfig struct {
	// Path is the file the store reads at construction and rewrites on
	// every Observe. Required, and caller-controlled: this package does
	// no directory-traversal filtering, auto-discovery, or directory
	// scanning, matching LoadFile's own established discipline — the
	// operator names the exact file.
	Path string `yaml:"path"`
}
