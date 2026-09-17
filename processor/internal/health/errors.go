package health

import "errors"

// Reasons readiness can be false without any dependency being at fault.
// Sentinels rather than ad-hoc strings so a caller can distinguish "not
// ready yet" from "the store is broken" when deciding what to log.
var (
	// ErrStarting means initialization has not finished. Expected, briefly,
	// on every start.
	ErrStarting = errors.New("health: runtime is still starting")

	// ErrDraining means shutdown has begun. Expected, and terminal.
	ErrDraining = errors.New("health: runtime is draining")
)
