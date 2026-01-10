package server

import (
	"context"
	"log/slog"
	"time"

	"embedding-sidecar/internal/detector"
	pb "embedding-sidecar/proto"
)

type EmbeddingHandler struct {
	pb.UnimplementedEmbeddingServiceServer
	detector *detector.Detector
}

func NewEmbeddingHandler(detector *detector.Detector) *EmbeddingHandler {
	return &EmbeddingHandler{detector: detector}
}

func (h *EmbeddingHandler) CheckLoop(ctx context.Context, req *pb.CheckLoopRequest) (*pb.CheckLoopResponse, error) {
	start := time.Now()
	if req == nil {
		return &pb.CheckLoopResponse{}, nil
	}
	result, err := h.detector.CheckLoop(ctx, req.GetTenantId(), req.GetPrompt())
	latency := time.Since(start)
	if err != nil {
		slog.Error("detector failed", "error", err, "latency_ms", latency.Milliseconds())
		return nil, err
	}
	slog.Info("loop check",
		"tenant_id", req.GetTenantId(),
		"loop_detected", result.LoopDetected,
		"max_similarity", result.MaxSimilarity,
		"latency_ms", latency.Milliseconds(),
	)
	return &pb.CheckLoopResponse{
		LoopDetected:  result.LoopDetected,
		MaxSimilarity: result.MaxSimilarity,
		SimilarPrompt: result.SimilarPrompt,
	}, nil
}
