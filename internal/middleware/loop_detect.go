package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"agent-sentinel/internal/loopdetect"
	"agent-sentinel/internal/parser"
	"agent-sentinel/internal/providers"
)

// LoopDetection middleware calls the embedding sidecar to detect loops and injects a hint on detection.
func LoopDetection(client *loopdetect.Client, provider providers.Provider, headerName, interventionHint string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client == nil || provider == nil || r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

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

			prompt := parser.ExtractFullRequestText(data)
			if prompt == "" {
				next.ServeHTTP(w, r)
				return
			}

			resp, err := client.Check(r.Context(), tenantID, prompt)
			if err != nil {
				slog.Warn("loop detect: sidecar check failed (fail-open)", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			if !resp.GetLoopDetected() {
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

			slog.Info("loop detected", "tenant_id", tenantID, "max_similarity", resp.GetMaxSimilarity(), "similar_prompt", resp.GetSimilarPrompt())
			next.ServeHTTP(w, r)
		})
	}
}
