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
)

// LoopDetection middleware calls the embedding sidecar to detect loops and injects a hint on detection.
func LoopDetection(client *loopdetect.Client, headerName, interventionHint string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client == nil || r.Method != http.MethodPost {
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

			if mutateRequestWithHint(data, interventionHint) {
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

func mutateRequestWithHint(data map[string]any, hint string) bool {
	if hint == "" {
		return false
	}
	contents, ok := data["contents"].([]any)
	if !ok || len(contents) == 0 {
		return false
	}
	first, ok := contents[0].(map[string]any)
	if !ok {
		return false
	}
	partsAny, ok := first["parts"].([]any)
	if !ok {
		partsAny = []any{}
	}
	hintPart := map[string]any{"text": hint}
	partsAny = append([]any{hintPart}, partsAny...)
	first["parts"] = partsAny
	contents[0] = first
	data["contents"] = contents
	return true
}
