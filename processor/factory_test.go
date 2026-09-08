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
