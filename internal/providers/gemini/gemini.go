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
