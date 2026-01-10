package gemini

import (
	"net/url"
	"testing"
)

func TestInjectHintAndExtraction(t *testing.T) {
	p := &Provider{base: &url.URL{}}
	body := map[string]any{
		"contents": []any{
			map[string]any{
				"parts": []any{
					map[string]any{"text": "hello"},
				},
			},
		},
	}
	ok := p.InjectHint(body, "hint")
	if !ok {
		t.Fatalf("expected inject hint to succeed")
	}
	first := body["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)
	if first["text"] != "hint" {
		t.Fatalf("expected hint at first position, got %v", first["text"])
	}
	if got := p.ExtractPrompt(body); got != "hint" {
		t.Fatalf("ExtractPrompt got %q", got)
	}
	if got := p.ExtractFullText(body); got != "hint hello" {
		t.Fatalf("ExtractFullText got %q", got)
	}
}

func TestExtractModelFromPath(t *testing.T) {
	p := &Provider{}
	model := p.ExtractModelFromPath("/v1beta/models/gemini-2.5-flash:generateContent")
	if model != "gemini-2.5-flash" {
		t.Fatalf("unexpected model %q", model)
	}
}

func TestParseTokenUsage(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"usageMetadata": map[string]any{
			"promptTokenCount":     float64(3),
			"candidatesTokenCount": float64(4),
		},
	}
	usage := p.ParseTokenUsage(body)
	if !usage.Found || usage.InputTokens != 3 || usage.OutputTokens != 4 {
		t.Fatalf("unexpected usage %+v", usage)
	}
}
