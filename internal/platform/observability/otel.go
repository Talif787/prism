// Package observability wires OpenTelemetry tracing for the service. Metrics and
// logs travel through the same collector in later phases; this package owns the
// tracer provider lifecycle and a clean shutdown hook.
package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
	Enabled      bool
	ServiceName  string
	Environment  string
	OTLPEndpoint string
	SamplerRatio float64
}

// Shutdown flushes and stops telemetry pipelines. It is safe to call once.
type Shutdown func(context.Context) error

// Setup installs global trace propagation and, when enabled, an OTLP tracer
// provider. When disabled it installs propagation only and a no-op shutdown, so
// the rest of the code path is identical in every environment.
func Setup(ctx context.Context, cfg Config) (Shutdown, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			attribute.String("deployment.environment.name", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	exp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("build otlp trace exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SamplerRatio))),
	)
	otel.SetTracerProvider(tp)

	return func(shutdownCtx context.Context) error {
		return errors.Join(tp.Shutdown(shutdownCtx), exp.Shutdown(shutdownCtx))
	}, nil
}
