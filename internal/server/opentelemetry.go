package server

import (
	"context"
	"errors"
	"time"

	"github.com/Michad/tilegroxy/pkg"
	"github.com/Michad/tilegroxy/pkg/static"
	"go.opentelemetry.io/otel"

	// "go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	// "go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	// "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const serviceName = "tilegroxy"

func setupOTelSDK(ctx context.Context) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	tracerProvider, err := newTraceProvider()
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	meterProvider, err := newMeterProvider()
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	loggerProvider, err := newLoggerProvider()
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

var instanceId = pkg.RandomString()

func standardAttributes() []attribute.KeyValue {
	version, _, _ := static.GetVersionInformation()

	return []attribute.KeyValue{
		semconv.ServiceNamespaceKey.String(serviceName),
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceVersionKey.String(version),
		semconv.ServiceInstanceIDKey.String(instanceId),
		semconv.TelemetrySDKLanguageGo,
	}
}

func newTraceProvider() (*trace.TracerProvider, error) {
	ctx := context.Background()
	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	traceRes, err := resource.New(ctx,
		resource.WithAttributes(standardAttributes()...),
	)

	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithResource(traceRes),
		trace.WithBatcher(traceExporter,
			trace.WithBatchTimeout(10*time.Second)),
	)
	return traceProvider, nil
}

func newMeterProvider() (*metric.MeterProvider, error) {
	ctx := context.Background()
	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	metricRes, err := resource.New(ctx,
		resource.WithAttributes(standardAttributes()...),
	)

	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(metricRes),
		metric.WithReader(metric.NewPeriodicReader(metricExporter,
			metric.WithInterval(time.Minute))),
	)
	return meterProvider, nil
}

func newLoggerProvider() (*log.LoggerProvider, error) {
	ctx := context.Background()
	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, err
	}

	loggingRes, err := resource.New(ctx,
		resource.WithAttributes(standardAttributes()...),
	)

	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithResource(loggingRes),
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
	)
	return loggerProvider, nil
}
