package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Sentinel errors for WebhookSink construction and delivery, checked
// with errors.Is per .claude/rules/go.md's error-handling convention.
var (
	// ErrMissingSecret is returned by NewWebhookSink when secret is
	// empty — a webhook with no signing secret cannot produce a
	// verifiable signature, so this fails at construction rather than
	// silently sending unsigned requests.
	ErrMissingSecret = errors.New("alert: webhook signing secret must not be empty")

	// ErrNonHTTPSDestination is returned by NewWebhookSink when
	// destination does not use the https scheme.
	ErrNonHTTPSDestination = errors.New("alert: webhook destination must use https")

	// ErrLoopbackDestination is returned by NewWebhookSink when
	// destination's host is a literal loopback, unspecified, or
	// link-local address. This is a basic, literal-IP check only — it
	// does not resolve hostnames via DNS (which would make
	// construction slow and flaky in tests, and is not a substitute for
	// real network-level egress control). Full SSRF protection is the
	// deploying application's responsibility, the same boundary
	// docs/SECURITY.md already draws for transport-layer concerns.
	ErrLoopbackDestination = errors.New("alert: webhook destination resolves to a loopback, unspecified, or link-local address")

	// ErrPayloadTooLarge is returned by Send when the serialized
	// Envelope exceeds maxPayloadBytes.
	ErrPayloadTooLarge = errors.New("alert: webhook payload exceeds maximum size")
)

const (
	// maxPayloadBytes bounds the outbound JSON payload. Alert's own
	// fields are bounded by construction (Reasons is at most
	// len(Anomaly.Contributors)+1, a small fixed-size list per
	// anomaly.Score's own bounded signal set), except Metadata, which
	// is caller-populated and open-ended — this bound is what stops a
	// misbehaving caller-supplied Metadata map from producing an
	// arbitrarily large outbound request.
	maxPayloadBytes = 64 * 1024

	// defaultTimeout is the WebhookSink default request timeout. A
	// bounded timeout is a security property as much as a performance
	// one: an unbounded call to an operator-configured, potentially
	// attacker-influenced destination is a resource-exhaustion vector
	// the core engine must not inherit.
	defaultTimeout = 5 * time.Second
)

// WebhookSink is a Sink that delivers an Alert as a signed, versioned
// JSON payload to a generic HTTPS webhook endpoint — the one delivery
// mechanism this Foundation stage ships (see
// docs/tasks/018-alert-notification-foundation.md § Non-Goals for
// every provider-specific sink deliberately not built here).
//
// WebhookSink performs exactly one delivery attempt per Send call: no
// retry, no backoff, no queue, no delivery-state tracking. That is the
// separately-scoped Reliability stage's job (see
// docs/ROADMAP.md § Alert & Notification phase), not this one's.
type WebhookSink struct {
	destination string
	secret      []byte
	httpClient  *http.Client
}

// webhookConfig collects everything WebhookOption can influence,
// including allowLoopback — which the loopback validation itself must
// see, so options are applied before validation, not after.
type webhookConfig struct {
	httpClient    *http.Client
	allowLoopback bool
}

// WebhookOption configures a WebhookSink at construction.
type WebhookOption func(*webhookConfig)

// WithHTTPClient overrides WebhookSink's default *http.Client — for
// tests, or an operator that needs custom transport settings (proxying,
// custom TLS config) beyond WithTimeout's single knob.
func WithHTTPClient(c *http.Client) WebhookOption {
	return func(cfg *webhookConfig) { cfg.httpClient = c }
}

// WithTimeout overrides WebhookSink's default request timeout
// (defaultTimeout).
func WithTimeout(d time.Duration) WebhookOption {
	return func(cfg *webhookConfig) { cfg.httpClient.Timeout = d }
}

// WithAllowLoopback disables the loopback/link-local destination check
// NewWebhookSink otherwise applies. This exists for a genuine,
// non-test use case, not only for tests against httptest servers (which
// always bind to 127.0.0.1): a local development relay (e.g. a
// container sidecar, a local n8n instance) is a real deployment shape,
// and rejecting it unconditionally would make the loopback check an
// obstacle rather than a safety default. HTTPS is still required
// regardless of this option.
func WithAllowLoopback() WebhookOption {
	return func(cfg *webhookConfig) { cfg.allowLoopback = true }
}

// NewWebhookSink constructs a WebhookSink, validating destination and
// secret eagerly: a malformed destination, a non-HTTPS destination, a
// destination resolving to a loopback/link-local literal address
// (unless WithAllowLoopback is given), or an empty secret fails
// construction, rather than failing silently the first time Send is
// called.
func NewWebhookSink(destination, secret string, opts ...WebhookOption) (*WebhookSink, error) {
	if secret == "" {
		return nil, ErrMissingSecret
	}

	cfg := webhookConfig{httpClient: &http.Client{Timeout: defaultTimeout}}
	for _, opt := range opts {
		opt(&cfg)
	}

	u, err := url.Parse(destination)
	if err != nil {
		return nil, fmt.Errorf("alert: parse webhook destination: %w", err)
	}
	if u.Scheme != "https" {
		return nil, ErrNonHTTPSDestination
	}
	if !cfg.allowLoopback {
		if err := checkNotLoopback(u.Hostname()); err != nil {
			return nil, err
		}
	}

	return &WebhookSink{
		destination: destination,
		secret:      []byte(secret),
		httpClient:  cfg.httpClient,
	}, nil
}

func checkNotLoopback(host string) error {
	if host == "localhost" {
		return ErrLoopbackDestination
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A non-literal hostname (the common case) is not resolved
		// here — see ErrLoopbackDestination's doc comment.
		return nil
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return ErrLoopbackDestination
	}
	return nil
}

// Send builds the versioned Envelope for a, signs it, and POSTs it to
// the configured destination. It performs no retry: a non-2xx response
// or a transport error is returned to the caller as-is.
func (s *WebhookSink) Send(ctx context.Context, a Alert) error {
	body, err := json.Marshal(NewEnvelope(a))
	if err != nil {
		return fmt.Errorf("alert: marshal webhook payload: %w", err)
	}
	if len(body) > maxPayloadBytes {
		return ErrPayloadTooLarge
	}

	ts := time.Now().Unix()
	deliveryID := newRandomID("dlv")
	signature := signPayload(s.secret, ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.destination, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("alert: build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trustvian-Signature", "sha256="+signature)
	req.Header.Set("X-Trustvian-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Trustvian-Alert-ID", a.ID)
	req.Header.Set("X-Trustvian-Delivery-ID", deliveryID)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("alert: send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("alert: webhook responded with status %d", resp.StatusCode)
	}
	return nil
}

// signPayload computes the hex-encoded HMAC-SHA256 over "<timestamp>.<body>",
// not over body alone — binding the timestamp into the signed content is
// what lets a receiver enforce a replay window (reject a request whose
// X-Trustvian-Timestamp is too old) without an attacker being able to
// simply attach a fresh timestamp to a previously-valid signature.
func signPayload(secret []byte, ts int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
