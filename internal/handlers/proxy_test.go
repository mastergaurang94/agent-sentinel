package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"agent-sentinel/internal/async"
	"agent-sentinel/internal/middleware"
	"agent-sentinel/internal/providers"
	"agent-sentinel/ratelimit"
)

type fakeLimiter struct {
	adjustEstimate float64
	adjustActual   float64
	refundEstimate float64
	adjustCh       chan struct{}
	refundCh       chan struct{}
}

func (f *fakeLimiter) AdjustCost(ctx context.Context, tenantID string, estimate, actual float64) error {
	f.adjustEstimate = estimate
	f.adjustActual = actual
	if f.adjustCh != nil {
		f.adjustCh <- struct{}{}
	}
	return nil
}
func (f *fakeLimiter) RefundEstimate(ctx context.Context, tenantID string, estimate float64) error {
	f.refundEstimate = estimate
	if f.refundCh != nil {
		f.refundCh <- struct{}{}
	}
	return nil
}
func (f *fakeLimiter) CheckLimitAndIncrement(ctx context.Context, tenantID string, estimatedCost float64) (*ratelimit.CheckLimitResult, error) {
	return nil, nil
}
func (f *fakeLimiter) GetPricing(provider, model string) (ratelimit.Pricing, bool) {
	return ratelimit.Pricing{}, false
}

type fakeProvider struct {
	usage providers.TokenUsage
}

func (f fakeProvider) Name() string                               { return "fake" }
func (f fakeProvider) BaseURL() *url.URL                          { return nil }
func (f fakeProvider) PrepareRequest(req *http.Request)           {}
func (f fakeProvider) InjectHint(map[string]any, string) bool     { return false }
func (f fakeProvider) ExtractModelFromPath(path string) string    { return "" }
func (f fakeProvider) ExtractPrompt(body map[string]any) string   { return "" }
func (f fakeProvider) ExtractFullText(body map[string]any) string { return "" }
func (f fakeProvider) ParseTokenUsage(body map[string]any) providers.TokenUsage {
	return f.usage
}

func TestCreateModifyResponseAdjustsCost(t *testing.T) {
	lim := &fakeLimiter{adjustCh: make(chan struct{}, 1)}
	defer func() { async.RunOverride = nil }()
	async.RunOverride = func(fn func()) { fn() }
	prov := fakeProvider{
		usage: providers.TokenUsage{InputTokens: 2, OutputTokens: 3, Found: true},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/models/m:call", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTenantID, "t1")
	ctx = context.WithValue(ctx, middleware.ContextKeyEstimate, float64(1.0))
	ctx = context.WithValue(ctx, middleware.ContextKeyPricing, ratelimit.Pricing{InputPrice: 1, OutputPrice: 1})
	ctx = context.WithValue(ctx, middleware.ContextKeyModel, "m")
	ctx = context.WithValue(ctx, middleware.ContextKeyReqStart, time.Now())
	req = req.WithContext(ctx)

	respBody := map[string]any{"usage": map[string]any{}}
	payload, _ := json.Marshal(respBody)
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(payload)),
		Request:    req,
		Header:     make(http.Header),
	}

	err := CreateModifyResponse(lim, prov)(resp)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	// async adjustment; wait briefly
	select {
	case <-lim.adjustCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for adjust")
	}
}

func TestCreateModifyResponseRefundsOnErrorNoUsage(t *testing.T) {
	lim := &fakeLimiter{refundCh: make(chan struct{}, 1)}
	defer func() { async.RunOverride = nil }()
	async.RunOverride = func(fn func()) { fn() }
	prov := fakeProvider{usage: providers.TokenUsage{Found: false}}
	req := httptest.NewRequest(http.MethodPost, "/v1/models/m:call", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTenantID, "t1")
	ctx = context.WithValue(ctx, middleware.ContextKeyEstimate, float64(2.5))
	ctx = context.WithValue(ctx, middleware.ContextKeyPricing, ratelimit.Pricing{InputPrice: 1, OutputPrice: 1})
	ctx = context.WithValue(ctx, middleware.ContextKeyModel, "m")
	req = req.WithContext(ctx)

	respBody := map[string]any{"error": map[string]any{"message": "fail"}}
	payload, _ := json.Marshal(respBody)
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(bytes.NewReader(payload)),
		Request:    req,
		Header:     make(http.Header),
	}

	err := CreateModifyResponse(lim, prov)(resp)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	select {
	case <-lim.refundCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for refund")
	}
	if lim.refundEstimate != 2.5 {
		t.Fatalf("expected refund 2.5, got %v", lim.refundEstimate)
	}
}

func TestErrorHandlerRefundsOnProxyError(t *testing.T) {
	lim := &fakeLimiter{refundCh: make(chan struct{}, 1)}
	defer func() { async.RunOverride = nil }()
	async.RunOverride = func(fn func()) { fn() }
	req := httptest.NewRequest(http.MethodPost, "/v1/models/m:call", nil)
	ctx := context.WithValue(req.Context(), middleware.ContextKeyTenantID, "t1")
	ctx = context.WithValue(ctx, middleware.ContextKeyEstimate, float64(3.3))
	ctx = context.WithValue(ctx, middleware.ContextKeyModel, "m")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	handler := CreateErrorHandler(lim)
	handler(rr, req, errors.New("proxy fail"))
	select {
	case <-lim.refundCh:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("timed out waiting for refund")
	}
	if lim.refundEstimate != 3.3 {
		t.Fatalf("expected refund 3.3, got %v", lim.refundEstimate)
	}
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
}
