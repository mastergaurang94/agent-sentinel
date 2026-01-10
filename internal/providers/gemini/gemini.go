package gemini

import (
	"net/http"
	"net/url"
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
