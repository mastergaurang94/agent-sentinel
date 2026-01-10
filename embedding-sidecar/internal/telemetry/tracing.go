package telemetry

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

var tracer trace.Tracer

// Init sets up OTLP tracing if endpoint is provided. It returns a shutdown func.
func Init(serviceName string) func(context.Context) error {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		slog.Info("OpenTelemetry disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)")
		tracer = otel.Tracer(serviceName)
		return func(context.Context) error { return nil }
	}

	ctx := context.Background()
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		slog.Warn("Failed to create OTLP exporter, tracing disabled", "error", err, "endpoint", endpoint)
		tracer = otel.Tracer(serviceName)
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		slog.Warn("Failed to create resource", "error", err)
		res = resource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	tracer = tp.Tracer(serviceName)

	slog.Info("OpenTelemetry tracing enabled", "endpoint", endpoint, "service", serviceName)
	return tp.Shutdown
}

// Tracer returns the configured tracer.
func Tracer() trace.Tracer {
	return tracer
}

// StartSpan starts a span if tracer is set; otherwise returns ctx, noop span.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// GRPCUnaryInterceptor returns the otelgrpc unary interceptor.
func GRPCUnaryInterceptor() grpc.UnaryServerInterceptor {
	return otelgrpc.UnaryServerInterceptor()
}
