package telemetry

import (
	"context"
	"log/slog"
	"runtime"
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
	ttftMs            metric.Float64Histogram
	streamDurationMs  metric.Float64Histogram
	providerLatencyMs metric.Float64Histogram
	providerErrors    metric.Int64Counter
	goroutinesGauge   metric.Int64ObservableGauge
	asyncQueueGauge   metric.Int64ObservableGauge
	gaugeOnce         sync.Once
	gaugeRegErr       error
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
		if ttftMs, err = meter.Float64Histogram("proxy.ttft_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "proxy.ttft_ms", "error", err)
		}
		if streamDurationMs, err = meter.Float64Histogram("proxy.stream.duration_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "proxy.stream.duration_ms", "error", err)
		}
		if providerLatencyMs, err = meter.Float64Histogram("proxy.provider_http.latency_ms"); err != nil {
			slog.Warn("failed to create metric", "name", "proxy.provider_http.latency_ms", "error", err)
		}
		if providerErrors, err = meter.Int64Counter("proxy.provider_http.errors"); err != nil {
			slog.Warn("failed to create metric", "name", "proxy.provider_http.errors", "error", err)
		}
		if goroutinesGauge, err = meter.Int64ObservableGauge("proxy.runtime.goroutines"); err != nil {
			slog.Warn("failed to create metric", "name", "proxy.runtime.goroutines", "error", err)
		}
		if asyncQueueGauge, err = meter.Int64ObservableGauge("proxy.async.queue_depth"); err != nil {
			slog.Warn("failed to create metric", "name", "proxy.async.queue_depth", "error", err)
		}
	})
}

// RegisterRuntimeGauges registers observable callbacks for goroutine count and async queue depth.
// queueDepthFn should return the current queue depth; pass nil if not available.
func RegisterRuntimeGauges(queueDepthFn func() int64) {
	gaugeOnce.Do(func() {
		if meter == nil {
			initMeter()
		}
		if goroutinesGauge != nil {
			_, gaugeRegErr = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
				o.ObserveInt64(goroutinesGauge, int64(runtime.NumGoroutine()))
				return nil
			}, goroutinesGauge)
		}
		if asyncQueueGauge != nil && queueDepthFn != nil {
			_, gaugeRegErr = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
				o.ObserveInt64(asyncQueueGauge, queueDepthFn())
				return nil
			}, asyncQueueGauge)
		}
		if gaugeRegErr != nil {
			slog.Warn("failed to register runtime gauges", "error", gaugeRegErr)
		}
	})
}

// RecordRateLimitRequest increments the rate limit request counter with outcome tags.
func RecordRateLimitRequest(ctx context.Context, result, reason, provider, model, tenantID string) {
	initMeter()
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	rateLimitRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ObserveRedisLatency records Redis operation latency in milliseconds.
func ObserveRedisLatency(ctx context.Context, op, backend, result string, d time.Duration, tenantID string) {
	initMeter()
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	redisLatencyMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
}

// IncRedisError increments the Redis error counter.
func IncRedisError(ctx context.Context, op, backend, tenantID string) {
	initMeter()
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	redisErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ObserveEstimateLatency records latency for token estimation.
func ObserveEstimateLatency(ctx context.Context, provider, model, tenantID string, d time.Duration) {
	initMeter()
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	estimateLatencyMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
}

// ObserveCostDelta records the difference between actual and estimated cost.
func ObserveCostDelta(ctx context.Context, provider, model, tenantID string, delta float64) {
	initMeter()
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	costDeltaUSD.Record(ctx, delta, metric.WithAttributes(attrs...))
}

// IncRefund increments the refund counter with a reason label.
func IncRefund(ctx context.Context, provider, model, tenantID, reason string) {
	initMeter()
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	refundCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// ObserveProviderHTTP records provider HTTP latency and errors with status/result attributes.
func ObserveProviderHTTP(ctx context.Context, provider, model string, status int, result string, d time.Duration) {
	initMeter()
	if providerLatencyMs == nil {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.String("result", result),
	}
	if model != "" {
		attrs = append(attrs, attribute.String("model", model))
	}
	if status > 0 {
		attrs = append(attrs, attribute.Int("http.status_code", status))
	}

	providerLatencyMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
	if result == "error" && providerErrors != nil {
		providerErrors.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

// ObserveTTFT records time-to-first-token latency for streaming responses.
func ObserveTTFT(ctx context.Context, provider, model, tenantID string, d time.Duration) {
	initMeter()
	if ttftMs == nil {
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	ttftMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
}

// ObserveStreamDuration records total streaming duration from request start to stream end.
func ObserveStreamDuration(ctx context.Context, provider, model, tenantID string, d time.Duration) {
	initMeter()
	if streamDurationMs == nil {
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
		attrs = append(attrs, attribute.String("tenant.id", tenantID))
	}

	streamDurationMs.Record(ctx, float64(d.Milliseconds()), metric.WithAttributes(attrs...))
}
