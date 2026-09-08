// Command trustvian-collector is a minimal, hand-assembled OpenTelemetry
// Collector binary that registers the trustvianprocessor alongside the
// standard OTLP receiver and the debug exporter. It exists so this
// module's example config.yaml (see ../../config.yaml) can actually run
// end-to-end, without depending on the external `ocb` (OpenTelemetry
// Collector Builder) tool — the Collector's own component APIs
// (go.opentelemetry.io/collector/otelcol) are enough to assemble a
// working binary directly.
//
// Run:
//
//	go run ./cmd/trustvian-collector --config=../../config.yaml
package main

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/confmap/provider/fileprovider"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/debugexporter"
	"go.opentelemetry.io/collector/otelcol"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"

	trustvianprocessor "trustvian-processor"
)

func components() (otelcol.Factories, error) {
	otlpFactory := otlpreceiver.NewFactory()
	debugFactory := debugexporter.NewFactory()
	trustvianFactory := trustvianprocessor.NewFactory()

	return otelcol.Factories{
		Receivers: map[component.Type]receiver.Factory{
			otlpFactory.Type(): otlpFactory,
		},
		Exporters: map[component.Type]exporter.Factory{
			debugFactory.Type(): debugFactory,
		},
		Processors: map[component.Type]processor.Factory{
			trustvianFactory.Type(): trustvianFactory,
		},
		Telemetry: newMinimalTelemetryFactory(),
	}, nil
}

func main() {
	configPath := "config.yaml"
	for _, arg := range os.Args[1:] {
		const prefix = "--config="
		if len(arg) > len(prefix) && arg[:len(prefix)] == prefix {
			configPath = arg[len(prefix):]
		}
	}

	settings := otelcol.CollectorSettings{
		BuildInfo: component.BuildInfo{
			Command:     "trustvian-collector",
			Description: "Minimal OTel Collector with the Trustvian processor",
			Version:     "0.0.0-dev",
		},
		Factories: components,
		ConfigProviderSettings: otelcol.ConfigProviderSettings{
			ResolverSettings: confmap.ResolverSettings{
				URIs:              []string{"file:" + configPath},
				ProviderFactories: []confmap.ProviderFactory{fileprovider.NewFactory()},
			},
		},
	}

	col, err := otelcol.NewCollector(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trustvian-collector: failed to construct: %v\n", err)
		os.Exit(1)
	}

	if err := col.Run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "trustvian-collector: run failed: %v\n", err)
		os.Exit(1)
	}
}
