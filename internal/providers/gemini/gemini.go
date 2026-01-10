package gemini

import (
	"net/http"
	"net/url"
	"strings"

	"agent-sentinel/internal/parser"
)

type Provider struct {
	base   *url.URL
	apiKey string
}

func New(apiKey string) (*Provider, error) {
	base, err := url.Parse("https://generativelanguage.googleapis.com")
	if err != nil {
		return nil, err
	}
	return &Provider{base: base, apiKey: apiKey}, nil
}

func (p *Provider) Name() string {
	return "gemini"
}

func (p *Provider) BaseURL() *url.URL {
	return p.base
}

func (p *Provider) PrepareRequest(req *http.Request) {
	q := req.URL.Query()
	q.Set("key", p.apiKey)
	req.URL.RawQuery = q.Encode()
	req.Host = p.base.Host
}

// InjectHint prepends a text hint to the first content part.
func (p *Provider) InjectHint(body map[string]any, hint string) bool {
	if hint == "" {
		return false
	}
	contents, ok := body["contents"].([]any)
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
	first["parts"] = append([]any{hintPart}, partsAny...)
	contents[0] = first
	body["contents"] = contents
	return true
}

func (p *Provider) ExtractModelFromPath(path string) string {
	modelsIndex := strings.Index(path, "/models/")
	if modelsIndex == -1 {
		return ""
	}
	afterModels := path[modelsIndex+8:]
	parts := strings.FieldsFunc(afterModels, func(r rune) bool {
		return r == '/' || r == ':'
	})
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}

func (p *Provider) ExtractPrompt(body map[string]any) string {
	if contents, ok := body["contents"].([]any); ok && len(contents) > 0 {
		if firstContent, ok := contents[0].(map[string]any); ok {
			if parts, ok := firstContent["parts"].([]any); ok && len(parts) > 0 {
				if firstPart, ok := parts[0].(map[string]any); ok {
					if text, ok := firstPart["text"].(string); ok {
						return text
					}
				}
			}
		}
	}
	return ""
}

func (p *Provider) ExtractFullText(body map[string]any) string {
	var parts []string
	if contents, ok := body["contents"].([]any); ok {
		for _, content := range contents {
			if contentMap, ok := content.(map[string]any); ok {
				if contentParts, ok := contentMap["parts"].([]any); ok {
					for _, part := range contentParts {
						if partMap, ok := part.(map[string]any); ok {
							if text, ok := partMap["text"].(string); ok {
								parts = append(parts, text)
							}
						}
					}
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

func (p *Provider) ParseTokenUsage(body map[string]any) parser.TokenUsage {
	if usage, ok := body["usageMetadata"].(map[string]any); ok {
		var inputTokens, outputTokens int
		if pt, ok := usage["promptTokenCount"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := usage["candidatesTokenCount"].(float64); ok {
			outputTokens = int(ct)
		}
		if inputTokens > 0 || outputTokens > 0 {
			return parser.TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens, Found: true}
		}
	}
	return parser.TokenUsage{}
}
