package openai

import (
	"net/url"
	"testing"
)

func TestInjectHintAndExtraction(t *testing.T) {
	p := &Provider{base: &url.URL{}}
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"input": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	ok := p.InjectHint(body, "system hint")
	if !ok {
		t.Fatalf("expected inject hint to succeed")
	}
	msgs := body["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "system hint" {
		t.Fatalf("unexpected first message %+v", first)
	}
	if got := p.ExtractPrompt(body); got != "hello" {
		t.Fatalf("ExtractPrompt got %q", got)
	}
	if got := p.ExtractFullText(body); got != "hello system hint hello" {
		t.Fatalf("ExtractFullText got %q", got)
	}
}

func TestExtractModelFromPath(t *testing.T) {
	p := &Provider{}
	model := p.ExtractModelFromPath("/v1beta/models/gpt-4o-mini:complete")
	if model != "gpt-4o-mini" {
		t.Fatalf("unexpected model %q", model)
	}
}

func TestParseTokenUsage(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(2),
			"completion_tokens": float64(5),
		},
	}
	usage := p.ParseTokenUsage(body)
	if !usage.Found || usage.InputTokens != 2 || usage.OutputTokens != 5 {
		t.Fatalf("unexpected usage %+v", usage)
	}
}
