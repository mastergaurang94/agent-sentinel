package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"agent-sentinel/internal/providers"
	"agent-sentinel/ratelimit"
)

type fakeProvider struct {
	model string
	text  string
}

func (f fakeProvider) Name() string                               { return "fake" }
func (f fakeProvider) BaseURL() *url.URL                          { return nil }
func (f fakeProvider) PrepareRequest(req *http.Request)           {}
func (f fakeProvider) InjectHint(map[string]any, string) bool     { return false }
func (f fakeProvider) ExtractModelFromPath(path string) string    { return f.model }
func (f fakeProvider) ExtractPrompt(body map[string]any) string   { return "" }
func (f fakeProvider) ExtractFullText(body map[string]any) string { return f.text }
func (f fakeProvider) ParseTokenUsage(body map[string]any) providers.TokenUsage {
	return providers.TokenUsage{}
}

type fakeLimiter struct {
	result *ratelimit.CheckLimitResult
	err    error
	refund float64
	adjust struct {
		estimate float64
		actual   float64
	}
}

func (f *fakeLimiter) CheckLimitAndIncrement(ctx context.Context, tenantID string, estimatedCost float64) (*ratelimit.CheckLimitResult, error) {
	return f.result, f.err
}
func (f *fakeLimiter) GetPricing(provider, model string) (ratelimit.Pricing, bool) {
	return ratelimit.Pricing{InputPrice: 1, OutputPrice: 1}, true
}
func (f *fakeLimiter) AdjustCost(ctx context.Context, tenantID string, estimate, actual float64) error {
	f.adjust.estimate = estimate
	f.adjust.actual = actual
	return nil
}
func (f *fakeLimiter) RefundEstimate(ctx context.Context, tenantID string, estimate float64) error {
	f.refund = estimate
	return nil
}

func TestRateLimitMiddlewareAllow(t *testing.T) {
	body := map[string]any{"model": "m", "contents": []any{map[string]any{"parts": []any{map[string]any{"text": "hi"}}}}}
	payload, _ := json.Marshal(body)

	limiter := &fakeLimiter{
		result: &ratelimit.CheckLimitResult{Allowed: true, Limit: 10, Remaining: 9},
	}
	prov := fakeProvider{model: "m", text: "hi"}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/m:generateContent", bytes.NewReader(payload))
	req.Header.Set("X-Tenant-ID", "t1")

	nextCalled := false
	handler := RateLimiting(limiter, prov, "X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		if r.Context().Value(ContextKeyTenantID) != "t1" {
			t.Fatalf("tenant missing in context")
		}
		if r.Context().Value(ContextKeyEstimate) == nil {
			t.Fatalf("estimate missing in context")
		}
	}))
	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Fatalf("next handler not called")
	}
	if rr.Code != 200 {
		t.Fatalf("unexpected status %d", rr.Code)
	}
	if rr.Header().Get("X-RateLimit-Limit") == "" {
		t.Fatalf("expected rate limit headers")
	}
}

func TestRateLimitMiddlewareDeny(t *testing.T) {
	body := map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": "hi"}}}}}
	payload, _ := json.Marshal(body)

	limiter := &fakeLimiter{
		result: &ratelimit.CheckLimitResult{Allowed: false, Limit: 1, Remaining: 0, CurrentSpend: 1},
	}
	prov := fakeProvider{text: "hi"}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/m:generateContent", bytes.NewReader(payload))
	req.Header.Set("X-Tenant-ID", "t1")

	handler := RateLimiting(limiter, prov, "X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("next should not be called on deny")
	}))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddlewareFailOpen(t *testing.T) {
	body := map[string]any{"contents": []any{map[string]any{"parts": []any{map[string]any{"text": "hi"}}}}}
	payload, _ := json.Marshal(body)

	limiter := &fakeLimiter{
		err: errors.New("redis down"),
	}
	prov := fakeProvider{text: "hi"}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/m:generateContent", bytes.NewReader(payload))
	req.Header.Set("X-Tenant-ID", "t1")

	nextCalled := false
	handler := RateLimiting(limiter, prov, "X-Tenant-ID")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))
	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Fatalf("expected next handler to be called on fail-open")
	}
	if rr.Code != 200 {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
}
