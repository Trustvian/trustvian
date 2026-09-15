// Command demo-producer emits a small, deterministic stream of OTLP spans
// for the reference Docker Compose deployment
// (deployments/docker-compose/) to demonstrate Trustvian learning and then
// scoring against persisted behavioral state.
//
// It is a telemetry producer and nothing else: no business logic, no HTTP
// server, no database. Every span it emits is shaped to map cleanly onto a
// Trustvian Event through the processor's own span mapping
// (processor/mapping.go), using only standard OpenTelemetry semantic
// conventions:
//
//	resource service.name                  -> Event.Actor.ID
//	resource deployment.environment.name   -> Event.Context.Environment
//	span name                              -> Event.Operation.Name
//	db.system.name / http.request.method   -> Event.Operation.Category
//	db.namespace / server.address          -> Event.Target.Name
//	span kind                              -> Event.Operation.Direction
//	span duration and status                -> duration_ms / error signals
//
// Determinism is the point. The operation set is fixed and cycled in
// order, so the same invocation always produces the same fingerprints in
// the same sequence — which is what makes "did the baseline persist?" a
// question with a checkable answer rather than an impression. Latency is
// varied by a fixed pattern rather than randomly for the same reason.
//
// Usage is entirely through environment variables so a Compose service
// definition needs no command line:
//
//	OTLP_ENDPOINT   collector address (default localhost:4317)
//	SPAN_COUNT      spans to emit (default 60)
//	SERVICE_NAME    resource service.name, i.e. the Trustvian actor
//	ENVIRONMENT     resource deployment.environment.name
//	ANOMALY         if "true", emit one deliberately abnormal span at the
//	                end — a slow call to an unfamiliar target
//
// It exits non-zero on any failure to connect or flush, so a smoke test
// can rely on its exit status instead of parsing its output.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// operation is one step in the repeating behavioral pattern. Each maps to
// a distinct Trustvian Fingerprint, so cycling through them builds
// transition and sequence state as well as per-operation statistics.
type operation struct {
	name string
	// attrs are the semantic-convention attributes that decide the
	// Event's operation category and target.
	attrs []attribute.KeyValue
	kind  trace.SpanKind
	// latency is fixed per operation so the learned baseline converges
	// instead of drifting — a producer with random latency would make
	// "the anomaly score changed" impossible to attribute.
	latency time.Duration
}

// normalPattern is the actor's routine behavior: three database reads and
// one outbound call, repeated. Deliberately small — the deployment
// demonstrates persistence, not the breadth of Trustvian's signals.
var normalPattern = []operation{
	{
		name: "SELECT orders",
		attrs: []attribute.KeyValue{
			semconv.DBSystemNameKey.String("postgresql"),
			semconv.DBNamespaceKey.String("orders-db"),
		},
		kind:    trace.SpanKindClient,
		latency: 12 * time.Millisecond,
	},
	{
		name: "SELECT order_items",
		attrs: []attribute.KeyValue{
			semconv.DBSystemNameKey.String("postgresql"),
			semconv.DBNamespaceKey.String("orders-db"),
		},
		kind:    trace.SpanKindClient,
		latency: 9 * time.Millisecond,
	},
	{
		name: "UPDATE order_status",
		attrs: []attribute.KeyValue{
			semconv.DBSystemNameKey.String("postgresql"),
			semconv.DBNamespaceKey.String("orders-db"),
		},
		kind:    trace.SpanKindClient,
		latency: 18 * time.Millisecond,
	},
	{
		name: "GET /shipping/quote",
		attrs: []attribute.KeyValue{
			semconv.HTTPRequestMethodKey.String("GET"),
			semconv.ServerAddressKey.String("shipping-api.internal"),
		},
		kind:    trace.SpanKindClient,
		latency: 40 * time.Millisecond,
	},
}

// anomalousOperation is an unfamiliar target reached slowly — the shape of
// something worth noticing. Emitted only when ANOMALY=true, so the normal
// run stays entirely routine and the two can be compared.
var anomalousOperation = operation{
	name: "GET /admin/export-all",
	attrs: []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String("GET"),
		semconv.ServerAddressKey.String("internal-admin-tools.example"),
	},
	kind:    trace.SpanKindClient,
	latency: 4 * time.Second,
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("demo-producer: %v", err)
	}
}

func run() error {
	endpoint := env("OTLP_ENDPOINT", "localhost:4317")
	serviceName := env("SERVICE_NAME", "demo-payments")
	environment := env("ENVIRONMENT", "production")

	count, err := strconv.Atoi(env("SPAN_COUNT", "60"))
	if err != nil || count < 1 {
		return fmt.Errorf("SPAN_COUNT must be a positive integer, got %q", env("SPAN_COUNT", ""))
	}

	ctx := context.Background()

	// WithInsecure because both ends live inside one Compose network on
	// one machine. A real deployment terminates TLS; see the reference
	// deployment's README § Security.
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("connect to collector at %s: %w", endpoint, err)
	}

	res, err := resource.New(ctx,
		// Empty base rather than resource.Default(): the default detector
		// adds host and process attributes that differ between runs and
		// between machines. They are harmless to Trustvian's mapping but
		// they make the emitted telemetry non-reproducible, which defeats
		// the purpose of a deterministic producer.
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.DeploymentEnvironmentNameKey.String(environment),
		),
	)
	if err != nil {
		return fmt.Errorf("build resource: %w", err)
	}

	// A simple (unbatched) span processor: every span is exported as it
	// ends. Slower than batching and exactly what is wanted here — it
	// makes the span count the producer reports the span count the
	// Collector actually received, with no flush timing to reason about.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSyncer(exporter),
	)
	tracer := tp.Tracer("trustvian-demo-producer")

	for i := range count {
		emit(ctx, tracer, normalPattern[i%len(normalPattern)])
	}

	emitted := count
	if env("ANOMALY", "false") == "true" {
		emit(ctx, tracer, anomalousOperation)
		emitted++
		log.Printf("demo-producer: emitted 1 deliberately anomalous span (%s)", anomalousOperation.name)
	}

	// Shutdown flushes. Its error is returned rather than logged: a
	// producer that exits 0 without delivering its spans would make every
	// downstream assertion meaningless.
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := tp.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("flush spans to %s: %w", endpoint, err)
	}

	log.Printf("demo-producer: sent %d spans to %s as actor %q in environment %q",
		emitted, endpoint, serviceName, environment)
	return nil
}

// emit produces one span whose recorded duration is op.latency. The
// duration is set explicitly with WithTimestamp rather than by sleeping:
// the producer stays fast, and the span's duration — which Trustvian reads
// as its latency signal — is exact rather than dependent on scheduling.
func emit(ctx context.Context, tracer trace.Tracer, op operation) {
	start := time.Now()
	_, span := tracer.Start(ctx, op.name,
		trace.WithSpanKind(op.kind),
		trace.WithAttributes(op.attrs...),
		trace.WithTimestamp(start),
	)
	span.End(trace.WithTimestamp(start.Add(op.latency)))
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
