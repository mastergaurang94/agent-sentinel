package openai

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"agent-sentinel/internal/providers"
)

type Provider struct {
	base   *url.URL
	apiKey string
}

func New(apiKey string) (*Provider, error) {
	base, err := url.Parse("https://api.openai.com")
	if err != nil {
		return nil, err
	}
	return &Provider{base: base, apiKey: apiKey}, nil
}

func (p *Provider) Name() string {
	return "openai"
}

func (p *Provider) BaseURL() *url.URL {
	return p.base
}

func (p *Provider) PrepareRequest(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))
	req.Host = p.base.Host
}

// InjectHint prepends a system message with the hint.
func (p *Provider) InjectHint(body map[string]any, hint string) bool {
	if hint == "" {
		return false
	}
	msgs, ok := body["messages"].([]any)
	if !ok {
		msgs = []any{}
	}
	hintMsg := map[string]any{"role": "system", "content": hint}
	body["messages"] = append([]any{hintMsg}, msgs...)
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
	if input, ok := body["input"]; ok {
		if inputStr, ok := input.(string); ok {
			return inputStr
		}
		if messages, ok := input.([]any); ok {
			for _, m := range messages {
				if msgMap, ok := m.(map[string]any); ok {
					if role, ok := msgMap["role"].(string); ok && role == "user" {
						if content, ok := msgMap["content"].(string); ok {
							return content
						}
					}
				}
			}
			if len(messages) > 0 {
				if msgMap, ok := messages[0].(map[string]any); ok {
					if content, ok := msgMap["content"].(string); ok {
						return content
					}
				}
			}
		}
	}
	return ""
}

func (p *Provider) ExtractFullText(body map[string]any) string {
	var parts []string
	if input, ok := body["input"]; ok {
		if inputStr, ok := input.(string); ok {
			parts = append(parts, inputStr)
		} else if messages, ok := input.([]any); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]any); ok {
					if content, ok := msgMap["content"].(string); ok {
						parts = append(parts, content)
					}
				}
			}
		}
	}
	if messages, ok := body["messages"].([]any); ok {
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]any); ok {
				if content, ok := msgMap["content"].(string); ok {
					parts = append(parts, content)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

func (p *Provider) ParseTokenUsage(body map[string]any) providers.TokenUsage {
	if usage, ok := body["usage"].(map[string]any); ok {
		var inputTokens, outputTokens int
		if pt, ok := usage["prompt_tokens"].(float64); ok {
			inputTokens = int(pt)
		}
		if ct, ok := usage["completion_tokens"].(float64); ok {
			outputTokens = int(ct)
		}
		if inputTokens > 0 || outputTokens > 0 {
			return providers.TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens, Found: true}
		}
	}
	return providers.TokenUsage{}
}
