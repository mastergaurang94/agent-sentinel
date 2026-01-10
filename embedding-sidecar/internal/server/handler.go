package server

import (
	"context"
	"log/slog"

	"embedding-sidecar/internal/detector"
	pb "embedding-sidecar/proto"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type EmbeddingHandler struct {
	pb.UnimplementedEmbeddingServiceServer
	detector *detector.Detector
}

var tracer = otel.Tracer("embedding-sidecar/server")

func NewEmbeddingHandler(detector *detector.Detector) *EmbeddingHandler {
	return &EmbeddingHandler{detector: detector}
}

func (h *EmbeddingHandler) CheckLoop(ctx context.Context, req *pb.CheckLoopRequest) (*pb.CheckLoopResponse, error) {
	ctx, span := tracer.Start(ctx, "CheckLoop")
	defer span.End()
	if req == nil {
		return &pb.CheckLoopResponse{}, nil
	}
	result, err := h.detector.CheckLoop(ctx, req.GetTenantId(), req.GetPrompt())
	if err != nil {
		slog.Error("detector failed", "error", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(
		attribute.Bool("loop.detected", result.LoopDetected),
		attribute.Float64("loop.max_similarity", result.MaxSimilarity),
	)
	return &pb.CheckLoopResponse{
		LoopDetected:  result.LoopDetected,
		MaxSimilarity: result.MaxSimilarity,
		SimilarPrompt: result.SimilarPrompt,
	}, nil
}
