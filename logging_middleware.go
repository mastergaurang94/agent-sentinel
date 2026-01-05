package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

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
