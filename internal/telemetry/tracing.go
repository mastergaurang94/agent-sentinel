package telemetry

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"agent-sentinel/internal/providers"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

// InitTracing configures OpenTelemetry if endpoint is provided.
func InitTracing() func(context.Context) error {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		slog.Info("OpenTelemetry disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)")
		tracer = otel.Tracer("agent-sentinel")
		return func(context.Context) error { return nil }
	}

	ctx := context.Background()

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		slog.Warn("Failed to create OTLP exporter, tracing disabled",
			"error", err,
			"endpoint", endpoint,
		)
		tracer = otel.Tracer("agent-sentinel")
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("agent-sentinel"),
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

	tracer = tp.Tracer("agent-sentinel")

	slog.Info("OpenTelemetry tracing enabled", "endpoint", endpoint)

	return tp.Shutdown
}

// Middleware wraps HTTP requests with tracing spans.
func Middleware(provider providers.Provider, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tracer == nil {
			next.ServeHTTP(w, r)
			return
		}

		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		ctx, span := tracer.Start(ctx, "llm_proxy_request",
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPath(r.URL.Path),
				semconv.HTTPRoute(r.URL.Path),
			),
		)
		defer span.End()

		// Add tenant/model attributes if present.
		if tenantID := r.Header.Get("X-Tenant-ID"); tenantID != "" {
			span.SetAttributes(attribute.String("tenant.id", tenantID))
		}
		if provider != nil {
			if model := provider.ExtractModelFromPath(r.URL.Path); model != "" {
				span.SetAttributes(attribute.String("llm.model", model))
			}
		}

		// Wrap response writer to capture status.
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r.WithContext(ctx))

		span.SetAttributes(semconv.HTTPResponseStatusCode(rw.statusCode))
		if rw.statusCode >= http.StatusBadRequest {
			span.SetAttributes(attribute.Bool("error", true))
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
