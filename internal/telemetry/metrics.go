package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	rateLimitRequests metric.Int64Counter
	redisLatencyMs    metric.Float64Histogram
	redisErrors       metric.Int64Counter
	estimateLatencyMs metric.Float64Histogram
	costDeltaUSD      metric.Float64Histogram
	refundCounter     metric.Int64Counter
)

// initMeter lazily initializes the meter and instruments. It uses the global
// meter provider, which will be a noop if metrics are not configured.
func initMeter() {
	meterOnce.Do(func() {
		meter = otel.Meter("agent-sentinel")

		var err error
		if rateLimitRequests, err = meter.Int64Counter("ratelimit.requests"); err != nil {
			slog.Warn("failed to create metric", "name", "ratelimit.requests", "error", err)
		}
		if redisLatencyMs, err = meter.Float64Histogram("ratelimit.redis.latency_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "ratelimit.redis.latency_ms", "error", err)
		}
		if redisErrors, err = meter.Int64Counter("ratelimit.redis.errors"); err != nil {
			slog.Warn("failed to create metric", "name", "ratelimit.redis.errors", "error", err)
		}
		if estimateLatencyMs, err = meter.Float64Histogram("ratelimit.estimate.latency_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "ratelimit.estimate.latency_ms", "error", err)
		}
		if costDeltaUSD, err = meter.Float64Histogram("ratelimit.cost.delta_usd"); err != nil {
			slog.Warn("failed to create metric", "name", "ratelimit.cost.delta_usd", "error", err)
		}
		if refundCounter, err = meter.Int64Counter("ratelimit.cost.refunds"); err != nil {
			slog.Warn("failed to create metric", "name", "ratelimit.cost.refunds", "error", err)
		}
	})
}

// RecordRateLimitRequest increments the rate limit request counter with outcome tags.
func RecordRateLimitRequest(ctx context.Context, result, reason, provider, model, tenantID string) {
	if rateLimitRequests == nil {
		initMeter()
	}
	if rateLimitRequests == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("result", result),
	}
	if reason != "" {
		attrs = append(attrs, attribute.String("reason", reason))
	}
	if provider != "" {
		attrs = append(attrs, attribute.String("provider", provider))
	}
	if model != "" {
		attrs = append(attrs, attribute.String("model", model))
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", hashTenant(tenantID)))
	}

	rateLimitRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ObserveRedisLatency records Redis operation latency in milliseconds.
func ObserveRedisLatency(ctx context.Context, op, backend, result string, d time.Duration, tenantID string) {
	if redisLatencyMs == nil {
		initMeter()
	}
	if redisLatencyMs == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("op", op),
		attribute.String("result", result),
	}
	if backend != "" {
		attrs = append(attrs, attribute.String("backend", backend))
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", hashTenant(tenantID)))
	}

	redisLatencyMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
}

// IncRedisError increments the Redis error counter.
func IncRedisError(ctx context.Context, op, backend, tenantID string) {
	if redisErrors == nil {
		initMeter()
	}
	if redisErrors == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("op", op),
	}
	if backend != "" {
		attrs = append(attrs, attribute.String("backend", backend))
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", hashTenant(tenantID)))
	}

	redisErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ObserveEstimateLatency records latency for token estimation.
func ObserveEstimateLatency(ctx context.Context, provider, model, tenantID string, d time.Duration) {
	if estimateLatencyMs == nil {
		initMeter()
	}
	if estimateLatencyMs == nil {
		return
	}

	attrs := []attribute.KeyValue{}
	if provider != "" {
		attrs = append(attrs, attribute.String("provider", provider))
	}
	if model != "" {
		attrs = append(attrs, attribute.String("model", model))
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", hashTenant(tenantID)))
	}

	estimateLatencyMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
}

// ObserveCostDelta records the difference between actual and estimated cost.
func ObserveCostDelta(ctx context.Context, provider, model, tenantID string, delta float64) {
	if costDeltaUSD == nil {
		initMeter()
	}
	if costDeltaUSD == nil {
		return
	}

	attrs := []attribute.KeyValue{}
	if provider != "" {
		attrs = append(attrs, attribute.String("provider", provider))
	}
	if model != "" {
		attrs = append(attrs, attribute.String("model", model))
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", hashTenant(tenantID)))
	}

	costDeltaUSD.Record(ctx, delta, metric.WithAttributes(attrs...))
}

// IncRefund increments the refund counter with a reason label.
func IncRefund(ctx context.Context, provider, model, tenantID, reason string) {
	if refundCounter == nil {
		initMeter()
	}
	if refundCounter == nil {
		return
	}

	attrs := []attribute.KeyValue{}
	if reason != "" {
		attrs = append(attrs, attribute.String("reason", reason))
	}
	if provider != "" {
		attrs = append(attrs, attribute.String("provider", provider))
	}
	if model != "" {
		attrs = append(attrs, attribute.String("model", model))
	}
	if tenantID != "" {
		attrs = append(attrs, attribute.String("tenant.id", hashTenant(tenantID)))
	}

	refundCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// hashTenant produces a short hash to avoid cardinality explosions while keeping IDs useful.
func hashTenant(tenantID string) string {
	sum := sha256.Sum256([]byte(tenantID))
	return hex.EncodeToString(sum[:])[:12]
}
