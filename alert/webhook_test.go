package alert_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Trustvian/trustvian/alert"
)

func TestNewWebhookSinkRejectsEmptySecret(t *testing.T) {
	_, err := alert.NewWebhookSink("https://example.com/hook", "")
	if err != alert.ErrMissingSecret {
		t.Errorf("err = %v, want %v", err, alert.ErrMissingSecret)
	}
}

func TestNewWebhookSinkRejectsNonHTTPS(t *testing.T) {
	_, err := alert.NewWebhookSink("http://example.com/hook", "s3cr3t")
	if err != alert.ErrNonHTTPSDestination {
		t.Errorf("err = %v, want %v", err, alert.ErrNonHTTPSDestination)
	}
}

func TestNewWebhookSinkRejectsMalformedURL(t *testing.T) {
	_, err := alert.NewWebhookSink("://not-a-url", "s3cr3t")
	if err == nil {
		t.Fatal("expected an error for a malformed destination URL")
	}
}

func TestNewWebhookSinkRejectsLoopbackDestination(t *testing.T) {
	tests := []string{
		"https://127.0.0.1/hook",
		"https://localhost/hook",
		"https://[::1]/hook",
		"https://0.0.0.0/hook",
		"https://169.254.1.1/hook",
	}
	for _, dest := range tests {
		t.Run(dest, func(t *testing.T) {
			_, err := alert.NewWebhookSink(dest, "s3cr3t")
			if err != alert.ErrLoopbackDestination {
				t.Errorf("err = %v, want %v", err, alert.ErrLoopbackDestination)
			}
		})
	}
}

func TestNewWebhookSinkAcceptsValidDestination(t *testing.T) {
	sink, err := alert.NewWebhookSink("https://hooks.example.com/trustvian", "s3cr3t")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sink == nil {
		t.Fatal("expected a non-nil sink")
	}
}

type capturedRequest struct {
	body    []byte
	headers http.Header
}

func newCapturingServer(t *testing.T, status int) (*httptest.Server, *capturedRequest, *sync.Mutex) {
	t.Helper()
	var mu sync.Mutex
	captured := &capturedRequest{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		captured.body = body
		captured.headers = r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, captured, &mu
}

// newTestSink builds a WebhookSink pointed at srv: srv.Client() trusts
// its self-signed certificate (WebhookSink requires real HTTPS), and
// WithAllowLoopback is required because httptest always binds to
// 127.0.0.1, which NewWebhookSink otherwise rejects.
func newTestSink(t *testing.T, srv *httptest.Server, secret string, extra ...alert.WebhookOption) *alert.WebhookSink {
	t.Helper()
	opts := append([]alert.WebhookOption{
		alert.WithHTTPClient(testHTTPClient(srv)),
		alert.WithAllowLoopback(),
	}, extra...)
	sink, err := alert.NewWebhookSink(srv.URL, secret, opts...)
	if err != nil {
		t.Fatalf("NewWebhookSink: %v", err)
	}
	return sink
}

// testHTTPClient returns an http.Client that trusts the httptest server's
// self-signed certificate, since WebhookSink otherwise requires a real
// HTTPS destination.
func testHTTPClient(srv *httptest.Server) *http.Client {
	return srv.Client()
}

func TestSendSignsPayloadCorrectly(t *testing.T) {
	srv, captured, mu := newCapturingServer(t, http.StatusOK)

	sink := newTestSink(t, srv, "s3cr3t")

	a := alert.New(sampleResult(), alert.SeverityCritical)
	if err := sink.Send(context.Background(), a); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	wantBody, err := json.Marshal(alert.NewEnvelope(a))
	if err != nil {
		t.Fatalf("marshal expected envelope: %v", err)
	}
	if string(captured.body) != string(wantBody) {
		t.Errorf("body = %s, want %s", captured.body, wantBody)
	}

	sig := captured.headers.Get("X-Trustvian-Signature")
	tsHeader := captured.headers.Get("X-Trustvian-Timestamp")
	if sig == "" || tsHeader == "" {
		t.Fatalf("missing signature/timestamp headers: sig=%q ts=%q", sig, tsHeader)
	}
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		t.Fatalf("timestamp header %q is not an integer: %v", tsHeader, err)
	}
	if age := time.Since(time.Unix(ts, 0)); age < 0 || age > 10*time.Second {
		t.Errorf("timestamp header age = %v, want a small positive duration", age)
	}

	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write([]byte(tsHeader))
	mac.Write([]byte("."))
	mac.Write(captured.body)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if sig != wantSig {
		t.Errorf("signature = %q, want %q", sig, wantSig)
	}

	if got := captured.headers.Get("X-Trustvian-Alert-ID"); got != a.ID {
		t.Errorf("X-Trustvian-Alert-ID = %q, want %q", got, a.ID)
	}
	if got := captured.headers.Get("X-Trustvian-Delivery-ID"); got == "" {
		t.Error("X-Trustvian-Delivery-ID header missing")
	}
	if got := captured.headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestSendTamperedPayloadFailsVerification(t *testing.T) {
	srv, captured, mu := newCapturingServer(t, http.StatusOK)
	sink := newTestSink(t, srv, "s3cr3t")

	a := alert.New(sampleResult(), alert.SeverityCritical)
	if err := sink.Send(context.Background(), a); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	sig := captured.headers.Get("X-Trustvian-Signature")
	ts := captured.headers.Get("X-Trustvian-Timestamp")
	tamperedBody := append([]byte(nil), captured.body...)
	mu.Unlock()

	// Flip one byte in the body a receiver would have gotten.
	tamperedBody[len(tamperedBody)-2] ^= 0xFF

	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(tamperedBody)
	recomputed := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if recomputed == sig {
		t.Error("tampering with the payload did not change the recomputed signature")
	}
}

func TestSendReturnsErrorOnNon2xx(t *testing.T) {
	srv, _, _ := newCapturingServer(t, http.StatusInternalServerError)
	sink := newTestSink(t, srv, "s3cr3t")

	err := sink.Send(context.Background(), alert.New(sampleResult(), alert.SeverityHigh))
	if err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not mention the response status", err)
	}
}

func TestSendRespectsTimeout(t *testing.T) {
	blockCh := make(chan struct{})

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blockCh
	}))
	// t.Cleanup runs LIFO: srv.Close() waits for the handler goroutine
	// to return, which is blocked on blockCh, so blockCh must be closed
	// (registered second, runs first) before srv.Close() (registered
	// first, runs second) — the reverse order deadlocks Close().
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(blockCh) })

	sink := newTestSink(t, srv, "s3cr3t", alert.WithTimeout(50*time.Millisecond))

	start := time.Now()
	err := sink.Send(context.Background(), alert.New(sampleResult(), alert.SeverityHigh))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Send took %v, expected it to abort near the configured 50ms timeout", elapsed)
	}
}

func TestSendPayloadTooLargeMakesNoNetworkCall(t *testing.T) {
	called := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	sink := newTestSink(t, srv, "s3cr3t")

	a := alert.New(sampleResult(), alert.SeverityHigh)
	a.Metadata = map[string]string{"blob": strings.Repeat("x", 128*1024)}

	err := sink.Send(context.Background(), a)
	if err != alert.ErrPayloadTooLarge {
		t.Errorf("err = %v, want %v", err, alert.ErrPayloadTooLarge)
	}
	if called {
		t.Error("Send made a network call despite an oversized payload")
	}
}

func TestSendDoesNotLeakSecret(t *testing.T) {
	srv, captured, mu := newCapturingServer(t, http.StatusOK)
	const secret = "super-secret-value-should-not-leak"
	sink := newTestSink(t, srv, secret)

	if err := sink.Send(context.Background(), alert.New(sampleResult(), alert.SeverityHigh)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if strings.Contains(string(captured.body), secret) {
		t.Error("outbound payload body contains the raw signing secret")
	}
	for name, values := range captured.headers {
		for _, v := range values {
			if strings.Contains(v, secret) {
				t.Errorf("header %q contains the raw signing secret: %q", name, v)
			}
		}
	}
}

func TestPayloadIsDeterministic(t *testing.T) {
	a := alert.New(sampleResult(), alert.SeverityCritical)

	b1, err := json.Marshal(alert.NewEnvelope(a))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b2, err := json.Marshal(alert.NewEnvelope(a))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b1) != string(b2) {
		t.Errorf("marshaling the same Alert twice produced different output:\n%s\nvs\n%s", b1, b2)
	}
}

func TestSendConcurrent(t *testing.T) {
	srv, _, _ := newCapturingServer(t, http.StatusOK)
	sink := newTestSink(t, srv, "s3cr3t")

	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a := alert.New(sampleResult(), alert.SeverityHigh)
			errCh <- sink.Send(context.Background(), a)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("concurrent Send failed: %v", err)
		}
	}
}
