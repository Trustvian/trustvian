package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	yaml "go.yaml.in/yaml/v3"
)

// Sentinel errors for the loader specifically, distinct from
// Validate's own — checked with errors.Is per .claude/rules/go.md.
var (
	// ErrEmptyInput is returned by Load when data has no content at
	// all — a distinct, actionable condition from a syntax error, and
	// clearer than the io.EOF the underlying YAML decoder would
	// otherwise surface for the same input.
	ErrEmptyInput = errors.New("config: empty input")

	// ErrFileTooLarge is returned by LoadFile when the file exceeds
	// maxConfigFileSize.
	ErrFileTooLarge = errors.New("config: file exceeds maximum size")
)

// maxConfigFileSize bounds LoadFile's input. Configuration is
// untrusted operational input — the same "resource exhaustion"
// reasoning maxRules/maxNameLength already apply to a PolicyConfig
// value apply here to the raw file before it is even decoded. 1 MiB
// is far beyond what any legitimate policy file needs (a file at
// maxRules=1000 rules, each with a realistic name/reason/condition,
// comes to a few hundred KB at most with generous YAML formatting) —
// large enough to never constrain a real policy, small enough to
// bound a pathological input (an accidentally-huge file, a special
// device file) to a fixed, cheap read.
const maxConfigFileSize = 1 << 20 // 1 MiB

// Load decodes data as a schema-v1 YAML document directly into a
// PolicyConfig, and validates the result before returning it — a
// caller receiving a (PolicyConfig, nil) from Load never needs to
// call Validate again before CompilePolicy, though doing so is
// harmless (CompilePolicy validates unconditionally regardless). Only
// YAML is supported; JSON is not a goal of this task (see
// docs/tasks/020-policy-config-loader.md § Non-Goals).
//
// Decoding is strict: an unrecognized field anywhere in the document
// fails with an error identifying it, rather than being silently
// ignored. This closes the exact security gap a config-time typo
// would otherwise open — see docs/SECURITY.md § Configuration-input
// validation for why a silently-ignored field is a silent policy
// weakening, not a cosmetic issue. Duplicate mapping keys (e.g. two
// "decision:" entries in the same rule) are also rejected — this is
// go.yaml.in/yaml/v3's own default behavior for a Decoder, verified
// empirically for this task, not assumed of the library; see
// TestLoadRejectsDuplicateKeys.
func Load(data []byte) (PolicyConfig, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return PolicyConfig{}, ErrEmptyInput
	}

	var cfg PolicyConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return PolicyConfig{}, ErrEmptyInput
		}
		return PolicyConfig{}, fmt.Errorf("config: decode: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return PolicyConfig{}, err
	}
	return cfg, nil
}

// LoadFile reads the file at path (bounded to maxConfigFileSize) and
// calls Load on its contents. path is caller-controlled — LoadFile
// does not perform its own directory-traversal filtering (a generic
// library helper enforcing a specific deployment's path policy would
// be the wrong layer for that decision), does not scan directories,
// and does not auto-discover a config file; the caller names the
// exact file to load.
func LoadFile(path string) (PolicyConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return PolicyConfig{}, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	// Read one byte beyond the limit so an over-limit file is
	// detected here rather than silently truncated and parsed as
	// whatever partial content happened to fit.
	data, err := io.ReadAll(io.LimitReader(f, maxConfigFileSize+1))
	if err != nil {
		return PolicyConfig{}, fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(data) > maxConfigFileSize {
		return PolicyConfig{}, fmt.Errorf("%w: %s (max %d bytes)", ErrFileTooLarge, path, maxConfigFileSize)
	}

	cfg, err := Load(data)
	if err != nil {
		return PolicyConfig{}, fmt.Errorf("config: %s: %w", path, err)
	}
	return cfg, nil
}
