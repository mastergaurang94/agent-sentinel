package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-sentinel/internal/async"
	"agent-sentinel/internal/handlers"
	"agent-sentinel/internal/loopdetect"
	"agent-sentinel/internal/middleware"
	"agent-sentinel/internal/providers"
	"agent-sentinel/internal/ratelimit"
	"agent-sentinel/internal/telemetry"
	pb "embedding-sidecar/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testTenantID = "tenant-integration"
	testModel    = "gemini-1.5-flash"
	loopHint     = "System: break the loop and respond with a new approach."
)

var asyncOnce sync.Once

func initAsyncAndTracing(t *testing.T) {
	t.Helper()
	asyncOnce.Do(func() {
		async.Init()
		async.RunOverride = func(fn func()) { fn() }
	})
	shutdown := telemetry.InitTracing()
	t.Cleanup(func() { _ = shutdown(context.Background()) })
}

func requireRedis(t *testing.T) *ratelimit.RedisClient {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL_INTEGRATION")
	if redisURL == "" {
		redisURL = "redis://localhost:6380"
	}
	t.Setenv("REDIS_URL", redisURL)
	client := ratelimit.NewRedisClient()
	if client == nil {
		t.Skipf("redis not reachable at %s", redisURL)
	}
	return client
}

func clearTenantSpend(t *testing.T, client *ratelimit.RedisClient, tenant string) {
	t.Helper()
	ctx := context.Background()
	_ = client.Client().Del(ctx, fmt.Sprintf("spend:%s", tenant)).Err()
	_ = client.Client().Del(ctx, fmt.Sprintf("limit:%s", tenant)).Err()
}

type testProvider struct {
	base  *url.URL
	model string
}

func (p testProvider) Name() string { return "gemini" }

func (p testProvider) BaseURL() *url.URL { return p.base }

func (p testProvider) PrepareRequest(req *http.Request) {}

func (p testProvider) InjectHint(body map[string]any, hint string) bool {
	msgs, ok := body["messages"].([]any)
	if !ok {
		msgs = []any{}
	}
	withHint := make([]any, 0, len(msgs)+1)
	withHint = append(withHint, map[string]any{"role": "system", "content": hint})
	withHint = append(withHint, msgs...)
	body["messages"] = withHint
	return true
}

func (p testProvider) ExtractModelFromPath(_ string) string {
	return p.model
}

func (p testProvider) ExtractPrompt(body map[string]any) string {
	return p.ExtractFullText(body)
}

func (p testProvider) ExtractFullText(body map[string]any) string {
	if body == nil {
		return ""
	}
	if msgs, ok := body["messages"].([]any); ok {
		var parts []string
		for _, raw := range msgs {
			if msg, ok := raw.(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					parts = append(parts, content)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	if text, ok := body["prompt"].(string); ok {
		return text
	}
	return ""
}

func (p testProvider) ParseTokenUsage(body map[string]any) providers.TokenUsage {
	usage, ok := body["usage"].(map[string]any)
	if !ok {
		return providers.TokenUsage{}
	}
	var in, out int
	if v, ok := usage["prompt_tokens"].(float64); ok {
		in = int(v)
	}
	if v, ok := usage["completion_tokens"].(float64); ok {
		out = int(v)
	}
	return providers.TokenUsage{
		InputTokens:  in,
		OutputTokens: out,
		Found:        in > 0 || out > 0,
	}
}

type recordedRequest struct {
	body []byte
}

func startBackend(t *testing.T) (*httptest.Server, <-chan recordedRequest) {
	t.Helper()
	reqCh := make(chan recordedRequest, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reqCh <- recordedRequest{body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	t.Cleanup(server.Close)
	return server, reqCh
}

func newProxyServer(t *testing.T, provider testProvider, limiter *ratelimit.RateLimiter, loopClient middleware.LoopClient, hint string) *httptest.Server {
	t.Helper()
	proxy := httputil.NewSingleHostReverseProxy(provider.BaseURL())
	original := proxy.Director
	proxy.Director = func(req *http.Request) {
		original(req)
		provider.PrepareRequest(req)
	}
	proxy.Transport = telemetry.NewInstrumentedTransport(provider, proxy.Transport)
	if limiter == nil {
		proxy.ModifyResponse = handlers.CreateModifyResponse(nil, provider)
		proxy.ErrorHandler = handlers.CreateErrorHandler(nil)
	} else {
		proxy.ModifyResponse = handlers.CreateModifyResponse(limiter, provider)
		proxy.ErrorHandler = handlers.CreateErrorHandler(limiter)
	}

	var handler http.Handler = proxy
	handler = middleware.Logging(provider, handler)
	handler = middleware.LoopDetection(loopClient, provider, "X-Tenant-ID", hint)(handler)
	if limiter == nil {
		handler = middleware.RateLimiting(nil, provider, "X-Tenant-ID")(handler)
	} else {
		handler = middleware.RateLimiting(limiter, provider, "X-Tenant-ID")(handler)
	}
	handler = telemetry.Middleware(provider, handler)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func waitForRequest(t *testing.T, ch <-chan recordedRequest) recordedRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(750 * time.Millisecond):
		t.Fatalf("backend not invoked")
		return recordedRequest{}
	}
}

func assertNoRequest(t *testing.T, ch <-chan recordedRequest) {
	t.Helper()
	select {
	case req := <-ch:
		t.Fatalf("backend should not be called, saw: %s", string(req.body))
	case <-time.After(200 * time.Millisecond):
	}
}

func makeRequestBody(prompt string, maxTokens int) []byte {
	payload := map[string]any{
		"model": testModel,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	if maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}
	b, _ := json.Marshal(payload)
	return b
}

func estimateCost(prompt, model string, maxTokens int) (float64, int, int, ratelimit.Pricing) {
	inputTokens := ratelimit.CountTokens(prompt, model)
	estimatedOutput := ratelimit.EstimateOutputTokens(inputTokens, maxTokens)
	pricing, ok := ratelimit.GetModelPricing("gemini", model)
	if !ok {
		pricing = ratelimit.DefaultPricing("gemini")
	}
	cost := ratelimit.CalculateCost(inputTokens, estimatedOutput, pricing)
	return cost, inputTokens, estimatedOutput, pricing
}

type loopServer struct {
	pb.UnimplementedEmbeddingServiceServer
	response   *pb.CheckLoopResponse
	errToSend  error
	callCount  *atomic.Int32
	latencyDur time.Duration
}

func (l *loopServer) CheckLoop(ctx context.Context, req *pb.CheckLoopRequest) (*pb.CheckLoopResponse, error) {
	if l.callCount != nil {
		l.callCount.Add(1)
	}
	if l.latencyDur > 0 {
		time.Sleep(l.latencyDur)
	}
	if l.errToSend != nil {
		return nil, l.errToSend
	}
	return l.response, nil
}

func startLoopUDSServer(t *testing.T, response *pb.CheckLoopResponse, errToSend error) (string, *atomic.Int32, func()) {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "aisock")
	if err != nil {
		t.Fatalf("mktemp sockets: %v", err)
	}
	udsPath := filepath.Join(base, "loop.sock")
	_ = os.Remove(udsPath)
	lis, err := net.Listen("unix", udsPath)
	if err != nil {
		t.Fatalf("listen uds: %v", err)
	}
	server := grpc.NewServer()
	callCounter := &atomic.Int32{}
	pb.RegisterEmbeddingServiceServer(server, &loopServer{
		response:  response,
		errToSend: errToSend,
		callCount: callCounter,
	})
	go server.Serve(lis)
	cleanup := func() {
		server.GracefulStop()
		_ = os.Remove(udsPath)
	}
	return udsPath, callCounter, cleanup
}

func TestIntegrationRateLimitAllow(t *testing.T) {
	initAsyncAndTracing(t)
	redisClient := requireRedis(t)
	clearTenantSpend(t, redisClient, testTenantID)

	prompt := "say hello in three languages"
	maxTokens := 20
	cost, _, _, _ := estimateCost(prompt, testModel, maxTokens)
	t.Setenv("DEFAULT_SPEND_LIMIT", fmt.Sprintf("%.6f", cost*5))

	limiter := ratelimit.NewRateLimiter(redisClient)
	if limiter == nil {
		t.Skip("rate limiter unavailable")
	}

	backend, reqCh := startBackend(t)
	baseURL, _ := url.Parse(backend.URL)
	provider := testProvider{base: baseURL, model: testModel}
	proxy := newProxyServer(t, provider, limiter, nil, loopHint)

	reqBody := makeRequestBody(prompt, maxTokens)
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/v1/test", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.Header.Get("X-RateLimit-Limit") == "" || resp.Header.Get("X-RateLimit-Remaining") == "" {
		t.Fatalf("expected rate limit headers to be set")
	}

	_ = waitForRequest(t, reqCh)
}

func TestIntegrationRateLimitDeny(t *testing.T) {
	initAsyncAndTracing(t)
	redisClient := requireRedis(t)
	clearTenantSpend(t, redisClient, testTenantID)

	prompt := strings.Repeat("deny me ", 20)
	maxTokens := 200
	cost, _, _, _ := estimateCost(prompt, testModel, maxTokens)
	t.Setenv("DEFAULT_SPEND_LIMIT", fmt.Sprintf("%.6f", cost/2))

	limiter := ratelimit.NewRateLimiter(redisClient)
	if limiter == nil {
		t.Skip("rate limiter unavailable")
	}

	backend, reqCh := startBackend(t)
	baseURL, _ := url.Parse(backend.URL)
	provider := testProvider{base: baseURL, model: testModel}
	proxy := newProxyServer(t, provider, limiter, nil, loopHint)

	reqBody := makeRequestBody(prompt, maxTokens)
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/v1/test", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	assertNoRequest(t, reqCh)
}

func TestIntegrationRateLimitFailOpenOnRedisError(t *testing.T) {
	initAsyncAndTracing(t)
	t.Setenv("REDIS_URL", "redis://localhost:0")
	t.Setenv("DEFAULT_SPEND_LIMIT", "1.0")

	limiter := ratelimit.NewRateLimiter(ratelimit.NewRedisClient())
	if limiter != nil {
		t.Fatalf("expected limiter to be nil when redis is unavailable")
	}

	backend, reqCh := startBackend(t)
	baseURL, _ := url.Parse(backend.URL)
	provider := testProvider{base: baseURL, model: testModel}
	proxy := newProxyServer(t, provider, nil, nil, loopHint)

	reqBody := makeRequestBody("redis down should pass", 50)
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/v1/test", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	_ = waitForRequest(t, reqCh)
}

func TestIntegrationLoopDetectionInjectsHint(t *testing.T) {
	initAsyncAndTracing(t)
	redisClient := requireRedis(t)
	clearTenantSpend(t, redisClient, testTenantID)

	prompt := "tell me a story"
	maxTokens := 64
	cost, _, _, _ := estimateCost(prompt, testModel, maxTokens)
	t.Setenv("DEFAULT_SPEND_LIMIT", fmt.Sprintf("%.6f", cost*5))
	limiter := ratelimit.NewRateLimiter(redisClient)
	if limiter == nil {
		t.Skip("rate limiter unavailable")
	}

	loopResp := &pb.CheckLoopResponse{
		LoopDetected:  true,
		MaxSimilarity: 0.92,
		SimilarPrompt: "tell me a story",
	}
	udsPath, calls, cleanup := startLoopUDSServer(t, loopResp, nil)
	defer cleanup()
	loopClient, err := loopdetect.New(udsPath, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("loop client init: %v", err)
	}

	backend, reqCh := startBackend(t)
	baseURL, _ := url.Parse(backend.URL)
	provider := testProvider{base: baseURL, model: testModel}
	proxy := newProxyServer(t, provider, limiter, loopClient, loopHint)

	reqBody := makeRequestBody(prompt, maxTokens)
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/v1/test", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", testTenantID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got := waitForRequest(t, reqCh)
	var payload map[string]any
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("decode forwarded body: %v", err)
	}
	msgs, ok := payload["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages with injected hint")
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(fmt.Sprint(first["content"]), "break the loop") {
		t.Fatalf("expected system hint injected, got first message: %#v", first)
	}
	if calls.Load() == 0 {
		t.Fatalf("expected loop sidecar to be called")
	}
}

func TestIntegrationLoopDetectionFailOpenAndRateLimitStillEnforces(t *testing.T) {
	initAsyncAndTracing(t)
	redisClient := requireRedis(t)
	clearTenantSpend(t, redisClient, testTenantID)

	prompt := "repeat this forever repeat this forever"
	maxTokens := 64
	cost, _, _, _ := estimateCost(prompt, testModel, maxTokens)
	// Second request will use a heavier payload to guarantee exceeding the budget.
	promptHeavy := prompt + strings.Repeat("!", 200)
	maxTokensHeavy := 512
	// Allow the first request, force the second to exceed the budget.
	t.Setenv("DEFAULT_SPEND_LIMIT", fmt.Sprintf("%.6f", cost*1.1))
	limiter := ratelimit.NewRateLimiter(redisClient)
	if limiter == nil {
		t.Skip("rate limiter unavailable")
	}

	udsPath, calls, cleanup := startLoopUDSServer(t, nil, status.Error(codes.Unavailable, "sidecar down"))
	defer cleanup()
	loopClient, err := loopdetect.New(udsPath, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("loop client init: %v", err)
	}

	backend, reqCh := startBackend(t)
	baseURL, _ := url.Parse(backend.URL)
	provider := testProvider{base: baseURL, model: testModel}
	proxy := newProxyServer(t, provider, limiter, loopClient, loopHint)

	doRequest := func(p string, maxT int) *http.Response {
		reqBody := makeRequestBody(p, maxT)
		req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/v1/test", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", testTenantID)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("proxy request failed: %v", err)
		}
		return resp
	}

	// First request: allowed (loop fails open)
	resp1 := doRequest(prompt, maxTokens)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", resp1.StatusCode)
	}
	resp1.Body.Close()
	_ = waitForRequest(t, reqCh)
	if calls.Load() == 0 {
		t.Fatalf("expected loop sidecar to be called on first request")
	}

	// Second request should be denied by rate limit despite loop fail-open.
	resp2 := doRequest(promptHeavy, maxTokensHeavy)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request expected 429, got %d", resp2.StatusCode)
	}
	assertNoRequest(t, reqCh)
	if calls.Load() != 1 {
		t.Fatalf("expected loop sidecar not to be invoked after rate limit denial; calls=%d", calls.Load())
	}
}

// Opt-in full-stack check that uses the real sidecar over UDS while keeping the provider stubbed.
// Skips unless RUN_FULLSTACK_SIDECAR=1 is set and the sidecar is reachable.
func TestIntegrationFullStack_WithRealSidecarOptIn(t *testing.T) {
	if os.Getenv("RUN_FULLSTACK_SIDECAR") != "1" {
		t.Skip("set RUN_FULLSTACK_SIDECAR=1 to run against real sidecar")
	}

	initAsyncAndTracing(t)
	redisClient := requireRedis(t)
	clearTenantSpend(t, redisClient, "tenant-fullstack")

	udsPath := os.Getenv("LOOP_EMBEDDING_SIDECAR_UDS")
	if udsPath == "" {
		udsPath = "/sockets/embedding-sidecar.sock"
	}

	loopClient, err := loopdetect.New(udsPath, 800*time.Millisecond)
	if err != nil || loopClient == nil {
		t.Skipf("sidecar not reachable at %s (%v)", udsPath, err)
	}

	// Warm and check sidecar directly; skip if it fails (keeps test opt-in and deterministic).
	tenantID := "tenant-fullstack"
	prompt := "tell me something interesting about embeddings"
	if _, err := loopClient.Check(context.Background(), tenantID, prompt); err != nil {
		t.Skipf("sidecar check failed (warmup): %v", err)
	}

	// Generous limit so rate limiting does not block the proxy path in this test.
	t.Setenv("DEFAULT_SPEND_LIMIT", "100.0")
	limiter := ratelimit.NewRateLimiter(redisClient)
	if limiter == nil {
		t.Skip("rate limiter unavailable")
	}

	backend, reqCh := startBackend(t)
	baseURL, _ := url.Parse(backend.URL)
	provider := testProvider{base: baseURL, model: testModel}
	proxy := newProxyServer(t, provider, limiter, loopClient, loopHint)

	reqBody := makeRequestBody(prompt, 128)
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/v1/test", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	got := waitForRequest(t, reqCh)
	var payload map[string]any
	if err := json.Unmarshal(got.body, &payload); err != nil {
		t.Fatalf("decode forwarded body: %v", err)
	}

	// If the real sidecar signaled a loop, we expect the system hint to be present.
	if respLoop, err := loopClient.Check(context.Background(), tenantID, prompt); err == nil && respLoop != nil && respLoop.LoopDetected {
		msgs, ok := payload["messages"].([]any)
		if !ok || len(msgs) == 0 {
			t.Fatalf("expected messages with injected hint when loop detected")
		}
		first, _ := msgs[0].(map[string]any)
		if first["role"] != "system" || !strings.Contains(fmt.Sprint(first["content"]), "break the loop") {
			t.Fatalf("expected system hint injected, got first message: %#v", first)
		}
	}
}
