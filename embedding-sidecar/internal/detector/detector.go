package detector

import (
	"context"
	"log/slog"

	"embedding-sidecar/internal/embedder"
	"embedding-sidecar/internal/store"
)

type Detector struct {
	store               *store.VectorStore
	embedder            embedder.Embedding
	similarityThreshold float64
	limit               int
}

type LoopResult struct {
	LoopDetected  bool
	MaxSimilarity float64
	SimilarPrompt string
}

func NewDetector(store *store.VectorStore, embedder embedder.Embedding, similarityThreshold float64, limit int) *Detector {
	return &Detector{
		store:               store,
		embedder:            embedder,
		similarityThreshold: similarityThreshold,
		limit:               limit,
	}
}

func (d *Detector) CheckLoop(ctx context.Context, tenantID, prompt string) (LoopResult, error) {
	embedding, err := d.embedder.Compute(prompt)
	if err != nil {
		return LoopResult{}, err
	}

	records, err := d.store.SearchSimilarEmbeddings(ctx, tenantID, embedding, d.limit)
	if err != nil {
		return LoopResult{}, err
	}

	var (
		maxSim        float64
		similarPrompt string
	)

	for _, rec := range records {
		if rec.Similarity > maxSim {
			maxSim = rec.Similarity
			similarPrompt = rec.Prompt
		}
	}

	// Store the new embedding asynchronously to keep latency low.
	go func() {
		storeCtx := context.WithoutCancel(ctx)
		if err := d.store.StoreEmbedding(storeCtx, tenantID, prompt, embedding); err != nil {
			slog.Warn("failed to store embedding", "error", err)
		}
	}()

	return LoopResult{
		LoopDetected:  maxSim > d.similarityThreshold,
		MaxSimilarity: maxSim,
		SimilarPrompt: similarPrompt,
	}, nil
}
