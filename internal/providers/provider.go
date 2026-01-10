package providers

import (
	"net/http"
	"net/url"
)

// Provider defines the minimal interface to prepare outbound requests to an LLM API.
type Provider interface {
	Name() string
	BaseURL() *url.URL
	PrepareRequest(req *http.Request)
}
