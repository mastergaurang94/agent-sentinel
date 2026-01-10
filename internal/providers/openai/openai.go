package openai

import (
	"fmt"
	"net/http"
	"net/url"
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
