package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"agent-sentinel/internal/async"
	"agent-sentinel/internal/middleware"
	"agent-sentinel/internal/parser"
	"agent-sentinel/internal/stream"
	"agent-sentinel/ratelimit"
)

// CreateModifyResponse builds the proxy ModifyResponse handler for cost tracking.
func CreateModifyResponse(limiter *ratelimit.RateLimiter) func(*http.Response) error {
	return func(resp *http.Response) error {
		if limiter == nil {
			return nil
		}

		ctx := resp.Request.Context()
		tenantID, _ := ctx.Value(middleware.ContextKeyTenantID).(string)
		estimate, _ := ctx.Value(middleware.ContextKeyEstimate).(float64)
		provider, _ := ctx.Value(middleware.ContextKeyProvider).(string)
		pricing, _ := ctx.Value(middleware.ContextKeyPricing).(ratelimit.Pricing)

		if tenantID == "" || estimate == 0 {
			return nil
		}

		if stream.IsStreamingResponse(resp) {
			streamReader := stream.NewStreamingResponseReader(resp.Body, provider, tenantID, estimate, pricing, limiter)
			resp.Body = streamReader
			slog.Debug("Streaming response detected, using chunk-based cost tracking",
				"tenant_id", tenantID,
				"estimate", estimate,
				"content_type", resp.Header.Get("Content-Type"),
			)
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Warn("Failed to read response body for cost tracking",
				"error", err,
				"tenant_id", tenantID,
			)
			return nil
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))

		var data map[string]any
		if err := json.Unmarshal(body, &data); err != nil {
			slog.Debug("Response not JSON, keeping estimate",
				"tenant_id", tenantID,
				"content_type", resp.Header.Get("Content-Type"),
			)
			return nil
		}

		isError := parser.HasErrorInResponse(data) || resp.StatusCode >= http.StatusBadRequest
		usage := parser.ParseTokenUsage(data, provider)

		async.Run(func() {
			bgCtx := context.Background()
			if usage.Found {
				actualCost := ratelimit.CalculateCost(usage.InputTokens, usage.OutputTokens, pricing)
				if err := limiter.AdjustCost(bgCtx, tenantID, estimate, actualCost); err != nil {
					slog.Warn("Failed to adjust cost",
						"error", err,
						"tenant_id", tenantID,
						"estimate", estimate,
						"actual", actualCost,
					)
				} else {
					slog.Debug("Cost adjusted",
						"tenant_id", tenantID,
						"estimate", estimate,
						"actual", actualCost,
						"input_tokens", usage.InputTokens,
						"output_tokens", usage.OutputTokens,
					)
				}
			} else if isError {
				if err := limiter.RefundEstimate(bgCtx, tenantID, estimate); err != nil {
					slog.Warn("Failed to refund estimate",
						"error", err,
						"tenant_id", tenantID,
						"estimate", estimate,
					)
				} else {
					slog.Debug("Estimate refunded (error with no usage)",
						"tenant_id", tenantID,
						"estimate", estimate,
						"status_code", resp.StatusCode,
					)
				}
			}
		})

		return nil
	}
}

// CreateErrorHandler builds the proxy error handler.
func CreateErrorHandler(limiter *ratelimit.RateLimiter) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, proxyErr error) {
		ctx := r.Context()
		tenantID, _ := ctx.Value(middleware.ContextKeyTenantID).(string)
		estimate, _ := ctx.Value(middleware.ContextKeyEstimate).(float64)

		if limiter != nil && tenantID != "" && estimate > 0 {
			async.Run(func() {
				bgCtx := context.Background()
				if refundErr := limiter.RefundEstimate(bgCtx, tenantID, estimate); refundErr != nil {
					slog.Warn("Failed to refund estimate on proxy error",
						"error", refundErr,
						"tenant_id", tenantID,
						"estimate", estimate,
					)
				} else {
					slog.Debug("Estimate refunded (proxy error)",
						"tenant_id", tenantID,
						"estimate", estimate,
						"proxy_error", proxyErr.Error(),
					)
				}
			})
		}

		slog.Error("Proxy error",
			"error", proxyErr,
			"tenant_id", tenantID,
		)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}
}
