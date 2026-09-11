package trustvianprocessor_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pipeline"
	"go.opentelemetry.io/collector/processor"

	trustvianprocessor "trustvian-processor"
)

func TestNewFactoryType(t *testing.T) {
	factory := trustvianprocessor.NewFactory()
	if got, want := factory.Type().String(), "trustvian"; got != want {
		t.Errorf("factory.Type() = %q, want %q", got, want)
	}
}

func TestNewFactoryCreatesTracesProcessor(t *testing.T) {
	factory := trustvianprocessor.NewFactory()
	cfg := factory.CreateDefaultConfig()
	if cfg == nil {
		t.Fatal("CreateDefaultConfig() = nil")
	}

	set := processor.Settings{
		ID:                component.NewID(component.MustNewType("trustvian")),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}
	proc, err := factory.CreateTraces(context.Background(), set, cfg, &capturingConsumer{})
	if err != nil {
		t.Fatalf("CreateTraces() error = %v", err)
	}
	if proc == nil {
		t.Fatal("CreateTraces() returned a nil processor with a nil error")
	}
}

// TestNewFactoryFailsOnInvalidPolicy proves this task's central
// security invariant: an explicitly configured `policy:` block that
// fails to decode/validate/compile must fail CreateTraces outright —
// non-zero error, no processor instance returned — never a silent
// fallback to the default Policy. This is what makes an invalid
// policy a whole-Collector startup failure (CreateTraces runs during
// pipeline graph construction, before Start), not something that
// surfaces only once the first span arrives.
func TestNewFactoryFailsOnInvalidPolicy(t *testing.T) {
	factory := trustvianprocessor.NewFactory()
	cfg := &trustvianprocessor.Config{
		Policy: map[string]any{
			"version": "v1",
			// default_decison (typo) instead of default_decision: an
			// explicit policy block missing its required default is
			// exactly the "invalid" case — decodePolicy will decode
			// it fine (it's a well-formed map), but
			// config.CompilePolicy's Validate call must reject it.
			"default_decison": "block",
			"default_reason":  "typo above must be rejected, not silently ignored",
		},
	}
	set := processor.Settings{
		ID:                component.NewID(component.MustNewType("trustvian")),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}

	proc, err := factory.CreateTraces(context.Background(), set, cfg, &capturingConsumer{})
	if err == nil {
		t.Fatal("CreateTraces() error = nil, want a non-nil error for an invalid policy config")
	}
	if proc != nil {
		t.Errorf("CreateTraces() processor = %v, want nil alongside the error", proc)
	}
}

// TestNewFactoryDoesNotSupportLogsOrMetrics confirms this is a
// traces-only processor, matching task 009's scope (traces in, traces
// enriched, traces out — no logs/metrics pipeline support was ever
// asked for).
func TestNewFactoryDoesNotSupportLogsOrMetrics(t *testing.T) {
	factory := trustvianprocessor.NewFactory()
	set := processor.Settings{
		ID:                component.NewID(component.MustNewType("trustvian")),
		TelemetrySettings: componenttest.NewNopTelemetrySettings(),
		BuildInfo:         component.NewDefaultBuildInfo(),
	}
	cfg := factory.CreateDefaultConfig()

	if _, err := factory.CreateLogs(context.Background(), set, cfg, consumer.Logs(nil)); err != pipeline.ErrSignalNotSupported {
		t.Errorf("CreateLogs() error = %v, want %v", err, pipeline.ErrSignalNotSupported)
	}
	if _, err := factory.CreateMetrics(context.Background(), set, cfg, consumer.Metrics(nil)); err != pipeline.ErrSignalNotSupported {
		t.Errorf("CreateMetrics() error = %v, want %v", err, pipeline.ErrSignalNotSupported)
	}
}
