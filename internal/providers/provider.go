package providers

import (
	"net/http"
	"net/url"

	"agent-sentinel/internal/parser"
)

// Provider defines the minimal interface to prepare outbound requests to an LLM API.
type Provider interface {
	Name() string
	BaseURL() *url.URL
	PrepareRequest(req *http.Request)
	InjectHint(body map[string]any, hint string) bool
	ExtractModelFromPath(path string) string
	ExtractPrompt(body map[string]any) string
	ExtractFullText(body map[string]any) string
	ParseTokenUsage(body map[string]any) parser.TokenUsage
}
