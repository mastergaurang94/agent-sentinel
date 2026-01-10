package detector

import (
	"context"
	"log/slog"

	"embedding-sidecar/internal/embedder"
	"embedding-sidecar/internal/store"
	"embedding-sidecar/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
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
	ctx, span := telemetry.StartSpan(ctx, "detector.check_loop",
		attribute.String("tenant.id", tenantID),
	)
	defer span.End()

	_, embedSpan := telemetry.StartSpan(ctx, "embedder.compute")
	embedding, err := d.embedder.Compute(prompt)
	if err != nil {
		embedSpan.RecordError(err)
		embedSpan.SetStatus(codes.Error, err.Error())
		embedSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return LoopResult{}, err
	}
	embedSpan.End()

	searchCtx, searchSpan := telemetry.StartSpan(ctx, "store.search",
		attribute.Int("search.limit", d.limit),
	)
	if err != nil {
		return LoopResult{}, err
	}

	records, err := d.store.SearchSimilarEmbeddings(searchCtx, tenantID, embedding, d.limit)
	if err != nil {
		searchSpan.RecordError(err)
		searchSpan.SetStatus(codes.Error, err.Error())
		searchSpan.End()
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return LoopResult{}, err
	}
	searchSpan.End()

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
		if err := d.store.StoreEmbedding(context.Background(), tenantID, prompt, embedding); err != nil {
			slog.Warn("failed to store embedding", "error", err)
		}
	}()

	result := LoopResult{
		LoopDetected:  maxSim > d.similarityThreshold,
		MaxSimilarity: maxSim,
		SimilarPrompt: similarPrompt,
	}
	span.SetAttributes(
		attribute.Bool("loop.detected", result.LoopDetected),
		attribute.Float64("loop.max_similarity", result.MaxSimilarity),
	)
	return result, nil
}
