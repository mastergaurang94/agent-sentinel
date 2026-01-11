package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"agent-sentinel/internal/providers"
	"agent-sentinel/internal/ratelimit"
	"agent-sentinel/internal/telemetry"
)

type ContextKey string

const (
	ContextKeyTenantID ContextKey = "rate_limit_tenant_id"
	ContextKeyEstimate ContextKey = "rate_limit_estimate"
	ContextKeyModel    ContextKey = "rate_limit_model"
	ContextKeyProvider ContextKey = "rate_limit_provider"
	ContextKeyPricing  ContextKey = "rate_limit_pricing"
	ContextKeyReqStart ContextKey = "request_start_time"
)

type RateLimiter interface {
	CheckLimitAndIncrement(ctx context.Context, tenantID string, estimatedCost float64) (*ratelimit.CheckLimitResult, error)
	GetPricing(provider, model string) (ratelimit.Pricing, bool)
}

func RateLimiting(limiter RateLimiter, provider providers.Provider, headerName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil || provider == nil || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			tenantID := r.Header.Get(headerName)
			if tenantID == "" {
				slog.Debug("No tenant ID in request, skipping rate limit",
					"header", headerName,
					"path", r.URL.Path,
				)
				next.ServeHTTP(w, r)
				return
			}

			// Record request start time once for downstream metrics (TTFT, duration).
			if _, ok := r.Context().Value(ContextKeyReqStart).(time.Time); !ok {
				r = r.WithContext(context.WithValue(r.Context(), ContextKeyReqStart, time.Now()))
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				slog.Error("Failed to read request body for rate limiting",
					"error", err,
					"tenant_id", tenantID,
				)
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			model := provider.ExtractModelFromPath(r.URL.Path)
			var data map[string]any
			if err := json.Unmarshal(body, &data); err == nil {
				if model == "" {
					if m, ok := data["model"].(string); ok {
						model = m
					}
				}
			}

			requestText := provider.ExtractFullText(data)
			if requestText == "" {
				slog.Debug("No text content found for token estimation",
					"tenant_id", tenantID,
					"model", model,
				)
				next.ServeHTTP(w, r)
				return
			}

			estStart := time.Now()
			inputTokens := ratelimit.CountTokens(requestText, model)

			pricing, found := limiter.GetPricing(provider.Name(), model)
			if !found {
				pricing = ratelimit.DefaultPricing(provider.Name())
				slog.Debug("Using default pricing for unknown model",
					"model", model,
					"provider", provider.Name(),
				)
			}

			maxOutputFromRequest := ratelimit.ExtractMaxOutputTokens(data)
			estimatedOutputTokens := ratelimit.EstimateOutputTokens(inputTokens, maxOutputFromRequest)
			estimatedCost := ratelimit.CalculateCost(inputTokens, estimatedOutputTokens, pricing)
			telemetry.ObserveEstimateLatency(r.Context(), provider.Name(), model, tenantID, time.Since(estStart))

			ctx := r.Context()
			result, err := limiter.CheckLimitAndIncrement(ctx, tenantID, estimatedCost)
			if err != nil {
				slog.Warn("Rate limit check failed, failing open",
					"error", err,
					"tenant_id", tenantID,
				)
				telemetry.RecordRateLimitRequest(ctx, "fail_open", "redis_error", provider.Name(), model, tenantID)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%.2f", result.Limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%.2f", result.Remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))

			if !result.Allowed {
				slog.Warn("Rate limit exceeded",
					"tenant_id", tenantID,
					"current_spend", result.CurrentSpend,
					"limit", result.Limit,
					"estimated_cost", estimatedCost,
				)
				telemetry.RecordRateLimitRequest(ctx, "denied", "over_limit", provider.Name(), model, tenantID)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{
						"message": "Rate limit exceeded. Hourly spend limit reached.",
						"type":    "rate_limit_error",
						"code":    "rate_limit_exceeded",
					},
					"current_spend": result.CurrentSpend,
					"limit":         result.Limit,
					"remaining":     result.Remaining,
				})
				return
			}

			ctx = context.WithValue(r.Context(), ContextKeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ContextKeyEstimate, estimatedCost)
			ctx = context.WithValue(ctx, ContextKeyModel, model)
			ctx = context.WithValue(ctx, ContextKeyProvider, provider)
			ctx = context.WithValue(ctx, ContextKeyPricing, pricing)
			r = r.WithContext(ctx)

			telemetry.RecordRateLimitRequest(ctx, "allowed", "ok", provider.Name(), model, tenantID)

			slog.Debug("Rate limit check passed",
				"tenant_id", tenantID,
				"estimated_cost", estimatedCost,
				"current_spend", result.CurrentSpend,
				"remaining", result.Remaining,
			)

			next.ServeHTTP(w, r)
		})
	}
}
