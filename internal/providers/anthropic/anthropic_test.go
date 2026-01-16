package anthropic

import (
	"net/http"
	"net/url"
	"testing"
)

func TestNew(t *testing.T) {
	p, err := New("test-key")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "anthropic")
	}
	if got := p.BaseURL().String(); got != "https://api.anthropic.com" {
		t.Errorf("BaseURL() = %q, want %q", got, "https://api.anthropic.com")
	}
}

func TestPrepareRequest(t *testing.T) {
	p, _ := New("test-api-key")
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	p.PrepareRequest(req)

	if got := req.Header.Get("x-api-key"); got != "test-api-key" {
		t.Errorf("x-api-key header = %q, want %q", got, "test-api-key")
	}
	if got := req.Header.Get("anthropic-version"); got != APIVersion {
		t.Errorf("anthropic-version header = %q, want %q", got, APIVersion)
	}
	if req.Host != "api.anthropic.com" {
		t.Errorf("Host = %q, want %q", req.Host, "api.anthropic.com")
	}
}

func TestInjectHint_NoExistingSystem(t *testing.T) {
	p := &Provider{base: &url.URL{}}
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	ok := p.InjectHint(body, "system hint")
	if !ok {
		t.Fatal("expected InjectHint to succeed")
	}
	if body["system"] != "system hint" {
		t.Errorf("system = %v, want %q", body["system"], "system hint")
	}
}

func TestInjectHint_ExistingStringSystem(t *testing.T) {
	p := &Provider{base: &url.URL{}}
	body := map[string]any{
		"system":   "existing system",
		"messages": []any{},
	}
	ok := p.InjectHint(body, "hint")
	if !ok {
		t.Fatal("expected InjectHint to succeed")
	}
	expected := "hint\n\nexisting system"
	if body["system"] != expected {
		t.Errorf("system = %q, want %q", body["system"], expected)
	}
}

func TestInjectHint_ExistingArraySystem(t *testing.T) {
	p := &Provider{base: &url.URL{}}
	body := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "existing"},
		},
		"messages": []any{},
	}
	ok := p.InjectHint(body, "hint")
	if !ok {
		t.Fatal("expected InjectHint to succeed")
	}
	systemArr := body["system"].([]any)
	if len(systemArr) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(systemArr))
	}
	first := systemArr[0].(map[string]any)
	if first["text"] != "hint" {
		t.Errorf("first block text = %v, want %q", first["text"], "hint")
	}
}

func TestInjectHint_EmptyHint(t *testing.T) {
	p := &Provider{base: &url.URL{}}
	body := map[string]any{}
	ok := p.InjectHint(body, "")
	if ok {
		t.Error("expected InjectHint to return false for empty hint")
	}
}

func TestExtractModelFromPath(t *testing.T) {
	p := &Provider{}
	// Anthropic doesn't use model in path, but test the fallback logic
	tests := []struct {
		path string
		want string
	}{
		{"/v1/messages", ""},
		{"/v1/models/claude-3-haiku:generate", "claude-3-haiku"},
	}
	for _, tt := range tests {
		got := p.ExtractModelFromPath(tt.path)
		if got != tt.want {
			t.Errorf("ExtractModelFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestExtractPrompt_StringContent(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "hello world"},
		},
	}
	if got := p.ExtractPrompt(body); got != "hello world" {
		t.Errorf("ExtractPrompt() = %q, want %q", got, "hello world")
	}
}

func TestExtractPrompt_ArrayContent(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello from array"},
				},
			},
		},
	}
	if got := p.ExtractPrompt(body); got != "hello from array" {
		t.Errorf("ExtractPrompt() = %q, want %q", got, "hello from array")
	}
}

func TestExtractPrompt_SkipsAssistant(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "assistant", "content": "I am assistant"},
			map[string]any{"role": "user", "content": "user message"},
		},
	}
	if got := p.ExtractPrompt(body); got != "user message" {
		t.Errorf("ExtractPrompt() = %q, want %q", got, "user message")
	}
}

func TestExtractFullText(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"system": "system prompt",
		"messages": []any{
			map[string]any{"role": "user", "content": "user message"},
			map[string]any{"role": "assistant", "content": "assistant response"},
		},
	}
	got := p.ExtractFullText(body)
	if got != "system prompt user message assistant response" {
		t.Errorf("ExtractFullText() = %q", got)
	}
}

func TestExtractFullText_ArraySystem(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "system part 1"},
			map[string]any{"type": "text", "text": "system part 2"},
		},
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	got := p.ExtractFullText(body)
	if got != "system part 1 system part 2 hello" {
		t.Errorf("ExtractFullText() = %q", got)
	}
}

func TestParseTokenUsage(t *testing.T) {
	p := &Provider{}
	body := map[string]any{
		"usage": map[string]any{
			"input_tokens":  float64(100),
			"output_tokens": float64(50),
		},
	}
	usage := p.ParseTokenUsage(body)
	if !usage.Found {
		t.Fatal("expected usage.Found to be true")
	}
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want %d", usage.InputTokens, 100)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want %d", usage.OutputTokens, 50)
	}
}

func TestParseTokenUsage_NoUsage(t *testing.T) {
	p := &Provider{}
	body := map[string]any{}
	usage := p.ParseTokenUsage(body)
	if usage.Found {
		t.Error("expected usage.Found to be false")
	}
}
