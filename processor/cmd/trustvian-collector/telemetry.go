package main

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/service/telemetry"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

// newMinimalTelemetryFactory builds the collector's own internal
// telemetry.Factory — logging, tracing, and metrics FOR the Collector
// process itself (not the trustvian.* enrichment, which is unrelated
// and lives in ../../attributes.go).
//
// This is deliberately a small, hand-built factory rather than
// go.opentelemetry.io/collector/service/telemetry/otelconftelemetry
// (the Collector's own full-featured default): that factory transitively
// pulls in cloud-provider resource detectors (AWS ECS/EKS, Azure, GCP)
// and a Kubernetes API client — several hundred extra dependency-graph
// entries — for a "minimal working version" (task 009's own words) that
// has no use for any of them. A production deployment that wants full
// auto-detected resource attributes and OTLP self-telemetry export can
// swap this factory for otelconftelemetry's; this one just gets a
// working binary running with a real zap logger and noop tracing/
// metrics, at the dependency cost the task's minimality bar asks for.
func newMinimalTelemetryFactory() telemetry.Factory {
	return telemetry.NewFactory(
		func() component.Config { return struct{}{} },
		telemetry.WithCreateLogger(func(context.Context, telemetry.LoggerSettings, component.Config) (*zap.Logger, component.ShutdownFunc, error) {
			logger, err := zap.NewProduction()
			if err != nil {
				return nil, nil, err
			}
			return logger, func(context.Context) error { return logger.Sync() }, nil
		}),
		telemetry.WithCreateResource(func(_ context.Context, set telemetry.Settings, _ component.Config) (pcommon.Resource, string, error) {
			res := pcommon.NewResource()
			res.Attributes().PutStr("service.name", set.BuildInfo.Command)
			res.Attributes().PutStr("service.version", set.BuildInfo.Version)
			return res, "", nil
		}),
		telemetry.WithCreateTracerProvider(func(context.Context, telemetry.TracerSettings, component.Config) (telemetry.TracerProvider, error) {
			return noopTracerProvider{TracerProvider: nooptrace.NewTracerProvider()}, nil
		}),
		telemetry.WithCreateMeterProvider(func(context.Context, telemetry.MeterSettings, component.Config) (telemetry.MeterProvider, error) {
			return noopMeterProvider{MeterProvider: noopmetric.NewMeterProvider()}, nil
		}),
	)
}

// noopTracerProvider/noopMeterProvider add the Shutdown method
// telemetry.TracerProvider/MeterProvider require on top of the OTel
// API's own noop providers, which have no such method (they do nothing
// to shut down).
type noopTracerProvider struct{ trace.TracerProvider }

func (noopTracerProvider) Shutdown(context.Context) error { return nil }

type noopMeterProvider struct{ metric.MeterProvider }

func (noopMeterProvider) Shutdown(context.Context) error { return nil }
