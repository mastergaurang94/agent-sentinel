package main

import (
	"context"
	"log/slog"

	pb "embedding-sidecar/proto"
)

type EmbeddingHandler struct {
	pb.UnimplementedEmbeddingServiceServer
	detector *Detector
}

func NewEmbeddingHandler(detector *Detector) *EmbeddingHandler {
	return &EmbeddingHandler{detector: detector}
}

func (h *EmbeddingHandler) CheckLoop(ctx context.Context, req *pb.CheckLoopRequest) (*pb.CheckLoopResponse, error) {
	if req == nil {
		return &pb.CheckLoopResponse{}, nil
	}
	result, err := h.detector.CheckLoop(ctx, req.GetTenantId(), req.GetPrompt())
	if err != nil {
		slog.Error("detector failed", "error", err)
		return nil, err
	}
	return &pb.CheckLoopResponse{
		LoopDetected:  result.LoopDetected,
		MaxSimilarity: result.MaxSimilarity,
		SimilarPrompt: result.SimilarPrompt,
	}, nil
}
