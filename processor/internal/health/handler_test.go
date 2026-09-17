package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

func TestHandlerLiveness(t *testing.T) {
	h := New(okProbe, time.Second)
	handler := Handler(h, nil)

	code, body := get(t, handler, PathLive)
	if code != http.StatusOK {
		t.Errorf("%s while starting = %d, want 200 — a starting runtime is alive", PathLive, code)
	}
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("%s body = %s, want status ok", PathLive, body)
	}

	h.MarkDraining()
	code, body = get(t, handler, PathLive)
	if code != http.StatusServiceUnavailable {
		t.Errorf("%s while draining = %d, want 503", PathLive, code)
	}
	if !strings.Contains(body, `"status":"stopping"`) {
		t.Errorf("%s body = %s, want status stopping", PathLive, body)
	}
}

func TestHandlerReadiness(t *testing.T) {
	tests := []struct {
		name     string
		setup    func() *Health
		wantCode int
		wantBody string
	}{
		{
			name:     "starting",
			setup:    func() *Health { return New(okProbe, time.Second) },
			wantCode: http.StatusServiceUnavailable,
			wantBody: `"status":"not_ready"`,
		},
		{
			name: "running and healthy",
			setup: func() *Health {
				h := New(okProbe, time.Second)
				h.MarkRunning()
				return h
			},
			wantCode: http.StatusOK,
			wantBody: `"status":"ok"`,
		},
		{
			name: "dependency unavailable",
			setup: func() *Health {
				h := New(failingProbe, time.Second)
				h.MarkRunning()
				return h
			},
			wantCode: http.StatusServiceUnavailable,
			wantBody: `"status":"not_ready"`,
		},
		{
			name: "no external dependency",
			setup: func() *Health {
				h := New(nil, time.Second)
				h.MarkRunning()
				return h
			},
			wantCode: http.StatusOK,
			wantBody: `"status":"ok"`,
		},
		{
			name: "draining",
			setup: func() *Health {
				h := New(okProbe, time.Second)
				h.MarkRunning()
				h.MarkDraining()
				return h
			},
			wantCode: http.StatusServiceUnavailable,
			wantBody: `"status":"not_ready"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body := get(t, Handler(tt.setup(), nil), PathReady)
			if code != tt.wantCode {
				t.Errorf("%s = %d, want %d", PathReady, code, tt.wantCode)
			}
			if !strings.Contains(body, tt.wantBody) {
				t.Errorf("%s body = %s, want %s", PathReady, body, tt.wantBody)
			}
		})
	}
}

// TestHandlerLeaksNothing is the §15/§16 security requirement asserted
// rather than assumed: the probe body must carry a status and nothing else.
// The probe here fails with an error full of exactly the things that must
// never reach an unauthenticated endpoint.
func TestHandlerLeaksNothing(t *testing.T) {
	secret := "postgres://trustvian:sup3rs3cret@db.internal:5432/trustvian?sslmode=require"
	leaky := func(context.Context) error {
		return errors.New("failed to connect to " + secret + ": FATAL: password authentication failed for user \"trustvian\" (SQLSTATE 28P01)")
	}

	h := New(leaky, time.Second)
	h.MarkRunning()

	var logged string
	handler := Handler(h, func(format string, args ...any) {
		logged = format
		_ = args
	})

	code, body := get(t, handler, PathReady)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("%s = %d, want 503", PathReady, code)
	}

	for _, forbidden := range []string{
		secret, "sup3rs3cret", "db.internal", "SQLSTATE", "password", "trustvian:", "FATAL",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("readiness body leaked %q:\n%s", forbidden, body)
		}
	}

	// The body must be exactly one known field — not merely free of the
	// strings this test happened to think of.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("readiness body is not JSON: %v", err)
	}
	if len(decoded) != 1 {
		t.Errorf("readiness body has %d fields, want exactly 1: %v", len(decoded), decoded)
	}
	if _, ok := decoded["status"]; !ok {
		t.Errorf("readiness body has no status field: %v", decoded)
	}

	// The detail belongs in the log, so an operator can still diagnose it.
	if logged == "" {
		t.Error("the probe failure was not logged; the operator loses the reason entirely")
	}
}

// TestHandlerDoesNotLogExpectedStates keeps a frequently-polled endpoint
// from filling logs during normal startup and shutdown.
func TestHandlerDoesNotLogExpectedStates(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func() *Health
	}{
		{"starting", func() *Health { return New(okProbe, time.Second) }},
		{"draining", func() *Health {
			h := New(okProbe, time.Second)
			h.MarkRunning()
			h.MarkDraining()
			return h
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			logged := false
			handler := Handler(tt.setup(), func(string, ...any) { logged = true })
			get(t, handler, PathReady)
			if logged {
				t.Errorf("%s logged a warning for an expected state", PathReady)
			}
		})
	}
}

func TestHandlerHeaders(t *testing.T) {
	h := New(nil, time.Second)
	h.MarkRunning()

	rec := httptest.NewRecorder()
	Handler(h, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, PathReady, nil))

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	// A cached probe answer is a stale probe answer.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}

func TestHandlerUnknownPath(t *testing.T) {
	h := New(nil, time.Second)
	if code, _ := get(t, Handler(h, nil), "/metrics"); code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404 — the health server serves only its two endpoints", code)
	}
}
