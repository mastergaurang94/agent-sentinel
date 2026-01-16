package anthropic

import (
	"net/http"
	"net/url"
	"strings"

	"agent-sentinel/internal/providers"
)

// APIVersion is the Anthropic API version header value.
// https://docs.anthropic.com/en/api/versioning
const APIVersion = "2023-06-01"

type Provider struct {
	base   *url.URL
	apiKey string
}

func New(apiKey string) (*Provider, error) {
	base, err := url.Parse("https://api.anthropic.com")
	if err != nil {
		return nil, err
	}
	return &Provider{base: base, apiKey: apiKey}, nil
}

func (p *Provider) Name() string {
	return "anthropic"
}

func (p *Provider) BaseURL() *url.URL {
	return p.base
}

func (p *Provider) PrepareRequest(req *http.Request) {
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", APIVersion)
	req.Host = p.base.Host
}

// InjectHint sets or prepends to the system field in the request body.
// Anthropic uses a top-level "system" field (string or array of content blocks).
func (p *Provider) InjectHint(body map[string]any, hint string) bool {
	if hint == "" {
		return false
	}
	existing, hasSystem := body["system"]
	if !hasSystem {
		body["system"] = hint
		return true
	}
	// If system is a string, prepend the hint
	if existingStr, ok := existing.(string); ok {
		body["system"] = hint + "\n\n" + existingStr
		return true
	}
	// If system is an array of content blocks, prepend a text block
	if existingArr, ok := existing.([]any); ok {
		hintBlock := map[string]any{"type": "text", "text": hint}
		body["system"] = append([]any{hintBlock}, existingArr...)
		return true
	}
	return false
}

// ExtractModelFromPath extracts the model from paths like /v1/messages
// Anthropic doesn't put the model in the path, so we return empty.
// The model is in the request body instead.
func (p *Provider) ExtractModelFromPath(path string) string {
	// Anthropic uses the request body for model specification, not the path.
	// However, for consistency, check if there's a /models/ segment.
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

// ExtractPrompt extracts the user's prompt from the messages array.
// Anthropic format: messages: [{role: "user", content: "text" | [{type: "text", text: "..."}]}]
func (p *Provider) ExtractPrompt(body map[string]any) string {
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return ""
	}
	// Find the first user message
	for _, msg := range messages {
		msgMap, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		if role != "user" {
			continue
		}
		// Content can be a string or array of content blocks
		if contentStr, ok := msgMap["content"].(string); ok {
			return contentStr
		}
		if contentArr, ok := msgMap["content"].([]any); ok {
			for _, block := range contentArr {
				if blockMap, ok := block.(map[string]any); ok {
					if blockMap["type"] == "text" {
						if text, ok := blockMap["text"].(string); ok {
							return text
						}
					}
				}
			}
		}
	}
	return ""
}

// ExtractFullText extracts all text content from system and messages.
func (p *Provider) ExtractFullText(body map[string]any) string {
	var parts []string

	// Extract system prompt
	if system, ok := body["system"].(string); ok {
		parts = append(parts, system)
	} else if systemArr, ok := body["system"].([]any); ok {
		for _, block := range systemArr {
			if blockMap, ok := block.(map[string]any); ok {
				if text, ok := blockMap["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
	}

	// Extract messages
	if messages, ok := body["messages"].([]any); ok {
		for _, msg := range messages {
			msgMap, ok := msg.(map[string]any)
			if !ok {
				continue
			}
			// Content can be a string or array of content blocks
			if contentStr, ok := msgMap["content"].(string); ok {
				parts = append(parts, contentStr)
			} else if contentArr, ok := msgMap["content"].([]any); ok {
				for _, block := range contentArr {
					if blockMap, ok := block.(map[string]any); ok {
						if text, ok := blockMap["text"].(string); ok {
							parts = append(parts, text)
						}
					}
				}
			}
		}
	}

	return strings.Join(parts, " ")
}

// ParseTokenUsage extracts token usage from Anthropic response.
// Anthropic format: usage: {input_tokens: N, output_tokens: N}
func (p *Provider) ParseTokenUsage(body map[string]any) providers.TokenUsage {
	usage, ok := body["usage"].(map[string]any)
	if !ok {
		return providers.TokenUsage{}
	}
	var inputTokens, outputTokens int
	if it, ok := usage["input_tokens"].(float64); ok {
		inputTokens = int(it)
	}
	if ot, ok := usage["output_tokens"].(float64); ok {
		outputTokens = int(ot)
	}
	if inputTokens > 0 || outputTokens > 0 {
		return providers.TokenUsage{InputTokens: inputTokens, OutputTokens: outputTokens, Found: true}
	}
	return providers.TokenUsage{}
}
