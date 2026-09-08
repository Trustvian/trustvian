package trustvianprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

// componentType is this processor's registered component type — the
// "trustvian" in a Collector config's `processors: trustvian:` block.
var componentType = component.MustNewType("trustvian")

// NewFactory returns the component.Factory for this processor, for a
// Collector binary or builder manifest to register alongside its
// other receivers/processors/exporters.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		componentType,
		createDefaultConfig,
		processor.WithTraces(createTracesProcessor, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{}
}

func createTracesProcessor(_ context.Context, set processor.Settings, _ component.Config, next consumer.Traces) (processor.Traces, error) {
	return newTrustvianProcessor(set.TelemetrySettings, next), nil
}
