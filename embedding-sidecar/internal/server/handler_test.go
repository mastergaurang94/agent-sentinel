package server

import (
	"context"
	"errors"
	"testing"

	"embedding-sidecar/internal/detector"
	"embedding-sidecar/internal/store"
	pb "embedding-sidecar/proto"
)

type fakeEmbedder struct {
	vec []float32
	err error
}

func (f fakeEmbedder) Compute(string) ([]float32, error) {
	return f.vec, f.err
}

type fakeStore struct {
	records   []store.EmbeddingRecord
	searchErr error
}

func (f *fakeStore) SearchSimilarEmbeddings(ctx context.Context, tenantID string, queryEmbedding []float32, limit int) ([]store.EmbeddingRecord, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.records, nil
}

func (f *fakeStore) StoreEmbedding(ctx context.Context, tenantID, prompt string, embedding []float32) error {
	return nil
}

func TestHandlerCheckLoopSuccess(t *testing.T) {
	fs := &fakeStore{records: nil}
	d := detector.NewDetector(fs, fakeEmbedder{vec: []float32{0.1}}, 0.9, 5)
	h := NewEmbeddingHandler(d)

	resp, err := h.CheckLoop(context.Background(), &pb.CheckLoopRequest{
		TenantId: "t1",
		Prompt:   "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetLoopDetected() {
		t.Fatalf("expected no loop, got detected")
	}
	if resp.GetMaxSimilarity() != 0 {
		t.Fatalf("expected max_similarity 0, got %v", resp.GetMaxSimilarity())
	}
}

func TestHandlerPropagatesDetectorError(t *testing.T) {
	fs := &fakeStore{records: nil}
	d := detector.NewDetector(fs, fakeEmbedder{err: errors.New("embed fail")}, 0.9, 5)
	h := NewEmbeddingHandler(d)

	resp, err := h.CheckLoop(context.Background(), &pb.CheckLoopRequest{
		TenantId: "t1",
		Prompt:   "hello",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if resp != nil {
		t.Fatalf("expected nil response on error")
	}
}
