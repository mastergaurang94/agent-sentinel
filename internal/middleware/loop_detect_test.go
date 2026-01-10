package middleware

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

	"agent-sentinel/internal/providers"
	pb "embedding-sidecar/proto"
)

type fakeLoopClient struct {
	resp *pb.CheckLoopResponse
	err  error
}

func (f *fakeLoopClient) Check(ctx context.Context, tenantID, prompt string) (*pb.CheckLoopResponse, error) {
	return f.resp, f.err
}

type fakeProviderLD struct {
	text string
}

func (f fakeProviderLD) Name() string                     { return "fake" }
func (f fakeProviderLD) BaseURL() *url.URL                { return nil }
func (f fakeProviderLD) PrepareRequest(req *http.Request) {}
func (f fakeProviderLD) InjectHint(body map[string]any, hint string) bool {
	body["hinted"] = hint
	return true
}
func (f fakeProviderLD) ExtractModelFromPath(path string) string    { return "" }
func (f fakeProviderLD) ExtractPrompt(body map[string]any) string   { return "" }
func (f fakeProviderLD) ExtractFullText(body map[string]any) string { return f.text }
func (f fakeProviderLD) ParseTokenUsage(body map[string]any) providers.TokenUsage {
	return providers.TokenUsage{}
}

func TestLoopDetectSkipNoTenant(t *testing.T) {
	client := &fakeLoopClient{}
	prov := fakeProviderLD{text: "hi"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{"body":1}`)))
	// no tenant header
	nextCalled := false
	handler := LoopDetection(client, prov, "X-Tenant-ID", "hint")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))
	handler.ServeHTTP(rr, req)
	if !nextCalled {
		t.Fatalf("expected next called")
	}
}

func TestLoopDetectInjectsOnDetect(t *testing.T) {
	client := &fakeLoopClient{
		resp: &pb.CheckLoopResponse{
			LoopDetected:  true,
			MaxSimilarity: 0.9,
		},
	}
	prov := fakeProviderLD{text: "hi"}
	body := map[string]any{"some": "body"}
	payload, _ := json.Marshal(body)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader(payload))
	req.Header.Set("X-Tenant-ID", "t1")

	nextCalled := false
	handler := LoopDetection(client, prov, "X-Tenant-ID", "hint")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		buf, _ := io.ReadAll(r.Body)
		if !bytes.Contains(buf, []byte("hint")) {
			t.Fatalf("expected hint injected")
		}
	}))
	handler.ServeHTTP(rr, req)
	if !nextCalled {
		t.Fatalf("expected next called")
	}
}

func TestLoopDetectFailOpen(t *testing.T) {
	client := &fakeLoopClient{err: errors.New("sidecar down")}
	prov := fakeProviderLD{text: "hi"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{"body":1}`)))
	req.Header.Set("X-Tenant-ID", "t1")

	nextCalled := false
	handler := LoopDetection(client, prov, "X-Tenant-ID", "hint")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))
	handler.ServeHTTP(rr, req)
	if !nextCalled {
		t.Fatalf("expected next called on fail-open")
	}
}
