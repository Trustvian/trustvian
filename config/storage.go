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

// Storage type values accepted by StorageConfig.Type.
//
// StorageTypePostgres is deliberately *recognized but not yet
// implemented*: CompileStorage returns ErrStorageTypeNotImplemented for
// it rather than ErrInvalidStorageType. This is a more honest failure
// than "unknown type" for an operator who read docs/ROADMAP.md § v0.8
// (which names PostgreSQL as the preferred persistent candidate) and
// reasonably tried it — and it reserves the exact config spelling, so
// the milestone's next slice is a purely additive change to
// CompileStorage, not a rename operators would have to migrate.
//
// Critically, it fails *closed*: an explicit request for an
// unimplemented backend is a startup error, never a silent downgrade to
// memory or file storage. Silently substituting a non-durable store for
// an explicitly requested durable one would lose exactly the learned
// state the operator asked to persist — see
// TestCompileStoragePostgresFailsClosedNeverFallsBack.
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
