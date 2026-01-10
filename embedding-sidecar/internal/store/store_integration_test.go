package store

import (
	"context"
	"os"
	"testing"
	"time"

	"embedding-sidecar/internal/embedder"
)

// These integration tests require Redis Stack with VSS (e.g., redis-stack at localhost:6380).

func TestVectorStoreIntegration_WithRedisStack(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL_INTEGRATION")
	if redisURL == "" {
		redisURL = "redis://localhost:6380"
	}

	store, err := NewVectorStore(redisURL, 5*time.Minute, 5)
	if err != nil {
		t.Skipf("skipping: redis not reachable (%v)", err)
	}
	ctx := context.Background()
	if err := store.EnsureIndex(ctx); err != nil {
		t.Skipf("skipping: redis index not available (%v)", err)
	}

	vec := make([]float32, embedder.EmbeddingDim)
	for i := range vec {
		vec[i] = 0.01 * float32(i+1)
	}

	tenant := "tenant-test"
	prompt := "hello world"

	if err := store.StoreEmbedding(ctx, tenant, prompt, vec); err != nil {
		t.Fatalf("StoreEmbedding error: %v", err)
	}

	records, err := store.SearchSimilarEmbeddings(ctx, tenant, vec, 3)
	if err != nil {
		t.Fatalf("SearchSimilarEmbeddings error: %v", err)
	}
	if len(records) == 0 {
		t.Fatalf("expected at least one record")
	}
	if records[0].Similarity < 0.99 {
		t.Fatalf("expected similarity >= 0.99, got %v", records[0].Similarity)
	}
}
