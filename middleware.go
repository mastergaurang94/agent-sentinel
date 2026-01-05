package main

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

	"agent-sentinel/ratelimit"
)

type contextKey string

const (
	ctxKeyTenantID contextKey = "rate_limit_tenant_id"
	ctxKeyEstimate contextKey = "rate_limit_estimate"
	ctxKeyModel    contextKey = "rate_limit_model"
	ctxKeyProvider contextKey = "rate_limit_provider"
	ctxKeyPricing  contextKey = "rate_limit_pricing"
)

func rateLimitingMiddleware(limiter *ratelimit.RateLimiter, provider, headerName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method != http.MethodPost {
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

			model := extractModelFromPath(r.URL.Path)
			var data map[string]any
			if err := json.Unmarshal(body, &data); err == nil {
				if model == "" {
					if m, ok := data["model"].(string); ok {
						model = m
					}
				}
			}

			requestText := extractFullRequestText(data)
			if requestText == "" {
				slog.Debug("No text content found for token estimation",
					"tenant_id", tenantID,
					"model", model,
				)
				next.ServeHTTP(w, r)
				return
			}

			inputTokens := ratelimit.CountTokens(requestText, model)

			pricing, found := limiter.GetPricing(provider, model)
			if !found {
				pricing = ratelimit.DefaultPricing(provider)
				slog.Debug("Using default pricing for unknown model",
					"model", model,
					"provider", provider,
				)
			}

			maxOutputFromRequest := ratelimit.ExtractMaxOutputTokens(data)
			estimatedOutputTokens := ratelimit.EstimateOutputTokens(inputTokens, maxOutputFromRequest)
			estimatedCost := ratelimit.CalculateCost(inputTokens, estimatedOutputTokens, pricing)

			ctx := context.Background()
			result, err := limiter.CheckLimitAndIncrement(ctx, tenantID, estimatedCost)
			if err != nil {
				slog.Warn("Rate limit check failed, failing open",
					"error", err,
					"tenant_id", tenantID,
				)
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
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "3600")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]any{
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

			ctx = context.WithValue(r.Context(), ctxKeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ctxKeyEstimate, estimatedCost)
			ctx = context.WithValue(ctx, ctxKeyModel, model)
			ctx = context.WithValue(ctx, ctxKeyProvider, provider)
			ctx = context.WithValue(ctx, ctxKeyPricing, pricing)
			r = r.WithContext(ctx)

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

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		model := extractModelFromPath(r.URL.Path)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("Failed to read request body",
				"error", err,
				"method", r.Method,
				"path", r.URL.Path,
			)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		var prompt string
		var data map[string]any
		if err := json.Unmarshal(body, &data); err == nil {
			if model == "" {
				if m, ok := data["model"].(string); ok {
					model = m
				}
			}
			prompt = extractPromptFromGeminiContents(data)
			if prompt == "" {
				prompt = extractPromptFromOpenAIResponses(data)
			}
		}

		if model != "" {
			slog.Info("LLM request",
				"model", model,
				"prompt", prompt,
				"method", r.Method,
				"path", r.URL.Path,
			)
		}

		next.ServeHTTP(w, r)
	})
}
