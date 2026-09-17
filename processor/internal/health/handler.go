package health

import (
	"encoding/json"
	"net/http"
)

// Endpoint paths. One convention, no aliases — /livez and /readyz are
// conventional for generic runtimes and carry no orchestrator-specific
// meaning.
const (
	PathLive  = "/livez"
	PathReady = "/readyz"
)

// response is the entire body of a probe reply.
//
// One field, on purpose. These endpoints are unauthenticated, so their
// information content is kept near zero: no DSN, no host, no driver error,
// no configuration, no actor or behavioral data. The reason a probe failed
// goes to the runtime's logs, where an operator already has access and a
// passing scanner does not.
type response struct {
	Status string `json:"status"`
}

const (
	statusOK       = "ok"
	statusNotReady = "not_ready"
	statusStopping = "stopping"
)

// Handler serves the liveness and readiness endpoints for h.
//
// Returns an http.Handler rather than registering globally, so the caller
// owns the server and its lifecycle — and so tests can exercise it with
// httptest and no listener at all.
func Handler(h *Health, logf func(string, ...any)) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(PathLive, func(w http.ResponseWriter, r *http.Request) {
		if h.Live() {
			writeStatus(w, http.StatusOK, statusOK)
			return
		}
		writeStatus(w, http.StatusServiceUnavailable, statusStopping)
	})

	mux.HandleFunc(PathReady, func(w http.ResponseWriter, r *http.Request) {
		ready, err := h.Ready(r.Context())
		if ready {
			writeStatus(w, http.StatusOK, statusOK)
			return
		}
		// Logged, not returned: the operator gets the detail, the endpoint
		// does not. Starting and draining are expected states and are not
		// worth logging on every poll.
		if logf != nil && err != nil && err != ErrStarting && err != ErrDraining {
			logf("trustvianprocessor: readiness probe failed: %v", err)
		}
		writeStatus(w, http.StatusServiceUnavailable, statusNotReady)
	})

	return mux
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json")
	// Probes are polled frequently and their answer changes; never let one
	// be served from a cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	// The body is a fixed-shape struct with no caller-supplied content, so
	// encoding cannot fail in a way worth handling — and the status code is
	// already written.
	_ = json.NewEncoder(w).Encode(response{Status: status})
}
