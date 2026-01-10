package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"agent-sentinel/internal/loopdetect"
	"agent-sentinel/internal/providers"
	"agent-sentinel/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// LoopDetection middleware calls the embedding sidecar to detect loops and injects a hint on detection.
func LoopDetection(client *loopdetect.Client, provider providers.Provider, headerName, interventionHint string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client == nil || provider == nil || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			ctx, span := telemetry.StartSpan(ctx, "loop_detection.middleware")
			defer span.End()

			tenantID := r.Header.Get(headerName)
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				slog.Warn("loop detect: failed to read body", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			var data map[string]any
			if err := json.Unmarshal(body, &data); err != nil {
				next.ServeHTTP(w, r)
				return
			}

			prompt := provider.ExtractFullText(data)
			if prompt == "" {
				next.ServeHTTP(w, r)
				return
			}

			resp, err := client.Check(ctx, tenantID, prompt)
			if err != nil {
				slog.Warn("loop detect: sidecar check failed (fail-open)", "error", err)
				if span != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
				}
				next.ServeHTTP(w, r)
				return
			}
			if resp == nil || !resp.GetLoopDetected() {
				if span != nil {
					span.SetAttributes(
						attribute.Bool("loop.detected", false),
						attribute.Float64("loop.max_similarity", 0),
					)
				}
				next.ServeHTTP(w, r)
				return
			}

			if provider.InjectHint(data, interventionHint) {
				updated, err := json.Marshal(data)
				if err == nil {
					r.Body = io.NopCloser(bytes.NewReader(updated))
					r.ContentLength = int64(len(updated))
					r.Header.Set("Content-Length", strconv.Itoa(len(updated)))
				}
			}

			if span != nil {
				span.SetAttributes(
					attribute.Bool("loop.detected", true),
					attribute.Float64("loop.max_similarity", resp.GetMaxSimilarity()),
				)
			}
			slog.Info("loop detected", "tenant_id", tenantID, "max_similarity", resp.GetMaxSimilarity(), "similar_prompt", resp.GetSimilarPrompt())
			next.ServeHTTP(w, r)
		})
	}
}

// telemetryTracer returns the global tracer; separated for testability.
func telemetryTracer() trace.Tracer {
	return telemetry.Tracer()
}
