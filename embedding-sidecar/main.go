package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"embedding-sidecar/internal/config"
	"embedding-sidecar/internal/detector"
	"embedding-sidecar/internal/embedder"
	"embedding-sidecar/internal/server"
	"embedding-sidecar/internal/store"
	pb "embedding-sidecar/proto"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	vectorStore, err := store.NewVectorStore(cfg.RedisURL, cfg.EmbeddingTTL, cfg.HistorySize, cfg.EmbeddingDim)
	if err != nil {
		slog.Error("failed to init redis", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := vectorStore.EnsureIndex(ctx); err != nil {
		slog.Error("failed to ensure redis index", "error", err)
		os.Exit(1)
	}

	emb, err := embedder.NewONNXEmbedder(cfg.EmbeddingModelPath, cfg.EmbeddingVocabPath, cfg.EmbeddingOutputName, cfg.EmbeddingDim)
	if err != nil {
		slog.Error("failed to init embedder", "error", err)
		os.Exit(1)
	}

	_, warmSpan := otel.Tracer("embedding-sidecar/main").Start(ctx, "EmbedderWarmup")
	if err := embedder.Warmup(emb); err != nil {
		warmSpan.RecordError(err)
		warmSpan.SetStatus(codes.Error, err.Error())
		slog.Error("embedder warmup failed", "error", err)
		warmSpan.End()
		os.Exit(1)
	}
	warmSpan.End()
	slog.Info("embedder warmup completed")

	det := detector.NewDetector(vectorStore, emb, cfg.SimilarityThreshold, cfg.HistorySize)
	handler := server.NewEmbeddingHandler(det)

	if err := removeIfExists(cfg.UDSPath); err != nil {
		slog.Error("failed to cleanup UDS path", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.UDSPath), 0o755); err != nil {
		slog.Error("failed to create uds dir", "path", filepath.Dir(cfg.UDSPath), "error", err)
		os.Exit(1)
	}

	lis, err := net.Listen("unix", cfg.UDSPath)
	if err != nil {
		slog.Error("failed to listen on uds", "path", cfg.UDSPath, "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterEmbeddingServiceServer(grpcServer, handler)

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)

	go func() {
		slog.Info("embedding sidecar gRPC server started", "uds", cfg.UDSPath)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server exited", "error", err)
		}
	}()

	// Mark serving after warmup and registrations completed.
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	waitForShutdown(grpcServer, cfg.UDSPath)
}

func waitForShutdown(grpcServer *grpc.Server, udsPath string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	grpcServer.GracefulStop()
	_ = removeIfExists(udsPath)
	slog.Info("embedding sidecar shutdown complete")
}

func removeIfExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}
