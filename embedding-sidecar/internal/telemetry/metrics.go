package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter     metric.Meter
	meterOnce sync.Once

	embedderLatency metric.Float64Histogram
	embedderErrors  metric.Int64Counter

	redisLatency metric.Float64Histogram
	redisErrors  metric.Int64Counter

	loopChecks metric.Int64Counter
)

func initMeter() {
	meterOnce.Do(func() {
		meter = otel.Meter("embedding-sidecar")

		var err error
		if embedderLatency, err = meter.Float64Histogram("sidecar.embedder.latency_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "sidecar.embedder.latency_ms", "error", err)
		}
		if embedderErrors, err = meter.Int64Counter("sidecar.embedder.errors"); err != nil {
			slog.Warn("failed to create metric", "name", "sidecar.embedder.errors", "error", err)
		}
		if redisLatency, err = meter.Float64Histogram("sidecar.redis.latency_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "sidecar.redis.latency_ms", "error", err)
		}
		if redisErrors, err = meter.Int64Counter("sidecar.redis.errors"); err != nil {
			slog.Warn("failed to create metric", "name", "sidecar.redis.errors", "error", err)
		}
		if loopChecks, err = meter.Int64Counter("sidecar.loop_check.requests"); err != nil {
			slog.Warn("failed to create metric", "name", "sidecar.loop_check.requests", "error", err)
		}
	})
}

func ObserveEmbedderLatency(ctx context.Context, dim int, outputName, result string, d time.Duration) {
	if embedderLatency == nil {
		initMeter()
	}
	if embedderLatency == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Int("embedder.dim", dim),
		attribute.String("embedder.output_name", outputName),
		attribute.String("result", result),
	}
	embedderLatency.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
	if result == "error" && embedderErrors != nil {
		embedderErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func ObserveRedisLatency(ctx context.Context, op, result, tenantID string, d time.Duration) {
	if redisLatency == nil {
		initMeter()
	}
	if redisLatency == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("op", op),
		attribute.String("result", result),
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}
	redisLatency.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
	if result == "error" && redisErrors != nil {
		redisErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func RecordLoopCheck(ctx context.Context, result, tenantID string) {
	if loopChecks == nil {
		initMeter()
	}
	if loopChecks == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("result", result),
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}
	loopChecks.Add(ctx, 1, metric.WithAttributes(attrs...))
}
