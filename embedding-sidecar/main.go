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

	"google.golang.org/grpc"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	vectorStore, err := store.NewVectorStore(cfg.RedisURL, cfg.EmbeddingTTL, cfg.HistorySize)
	if err != nil {
		slog.Error("failed to init redis", "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := vectorStore.EnsureIndex(ctx); err != nil {
		slog.Error("failed to ensure redis index", "error", err)
		os.Exit(1)
	}

	emb, err := embedder.NewONNXEmbedder(cfg.EmbeddingModelPath, cfg.EmbeddingVocabPath)
	if err != nil {
		slog.Error("failed to init embedder", "error", err)
		os.Exit(1)
	}

	if err := embedder.Warmup(emb); err != nil {
		slog.Error("embedder warmup failed", "error", err)
		os.Exit(1)
	}
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

	go func() {
		slog.Info("embedding sidecar gRPC server started", "uds", cfg.UDSPath)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server exited", "error", err)
		}
	}()

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
