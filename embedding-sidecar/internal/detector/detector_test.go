package detector

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"embedding-sidecar/internal/store"
)

type fakeEmbedder struct {
	vec []float32
	err error
}

func (f fakeEmbedder) Compute(string) ([]float32, error) {
	return f.vec, f.err
}

type fakeStore struct {
	records    []store.EmbeddingRecord
	searchErr  error
	storeErr   error
	storeCalls int
	mu         sync.Mutex
}

func (f *fakeStore) SearchSimilarEmbeddings(ctx context.Context, tenantID string, queryEmbedding []float32, limit int) ([]store.EmbeddingRecord, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.records, nil
}

func (f *fakeStore) StoreEmbedding(ctx context.Context, tenantID, prompt string, embedding []float32) error {
	f.mu.Lock()
	f.storeCalls++
	f.mu.Unlock()
	return f.storeErr
}

func TestDetectorDetectsLoop(t *testing.T) {
	store := &fakeStore{
		records: []store.EmbeddingRecord{
			{Similarity: 0.97, Prompt: "prev"},
			{Similarity: 0.5, Prompt: "other"},
		},
	}
	d := NewDetector(store, fakeEmbedder{vec: []float32{0.1}}, 0.95, 5)
	res, err := d.CheckLoop(context.Background(), "tenant", "prompt")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.LoopDetected || res.MaxSimilarity != 0.97 || res.SimilarPrompt != "prev" {
		t.Fatalf("unexpected result: %+v", res)
	}
	waitForStore(t, store)
}

func TestDetectorNotDetected(t *testing.T) {
	store := &fakeStore{
		records: []store.EmbeddingRecord{
			{Similarity: 0.5, Prompt: "prev"},
		},
	}
	d := NewDetector(store, fakeEmbedder{vec: []float32{0.1}}, 0.95, 5)
	res, err := d.CheckLoop(context.Background(), "tenant", "prompt")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.LoopDetected || res.MaxSimilarity != 0.5 {
		t.Fatalf("unexpected result: %+v", res)
	}
	waitForStore(t, store)
}

func TestDetectorPropagatesErrors(t *testing.T) {
	d1 := NewDetector(&fakeStore{}, fakeEmbedder{err: errors.New("embed fail")}, 0.95, 5)
	if _, err := d1.CheckLoop(context.Background(), "tenant", "prompt"); err == nil {
		t.Fatalf("expected embedder error")
	}

	d2 := NewDetector(&fakeStore{searchErr: errors.New("search fail")}, fakeEmbedder{vec: []float32{0.1}}, 0.95, 5)
	if _, err := d2.CheckLoop(context.Background(), "tenant", "prompt"); err == nil {
		t.Fatalf("expected store error")
	}
}

func waitForStore(t *testing.T, fs *fakeStore) {
	t.Helper()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		fs.mu.Lock()
		calls := fs.storeCalls
		fs.mu.Unlock()
		if calls > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("store not called")
}
