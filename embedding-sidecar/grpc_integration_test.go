package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "embedding-sidecar/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type stubEmbedder struct {
	vec []float32
}

func (s *stubEmbedder) Compute(text string) ([]float32, error) {
	return s.vec, nil
}

func TestGRPCIntegration_CheckLoop(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL_INTEGRATION")
	if redisURL == "" {
		redisURL = "redis://localhost:6380"
	}

	store, err := NewVectorStore(redisURL, 5*time.Minute)
	if err != nil {
		t.Skipf("skipping: redis not reachable (%v)", err)
	}
	ctx := context.Background()
	if err := store.EnsureIndex(ctx); err != nil {
		t.Skipf("skipping: redis index not available (%v)", err)
	}

	vec := make([]float32, embeddingDim)
	for i := range vec {
		vec[i] = 0.02 * float32(i+1)
	}
	embedder := &stubEmbedder{vec: vec}

	detector := NewDetector(store, embedder, 0.5, 5)
	handler := NewEmbeddingHandler(detector)

	udsPath := filepath.Join(os.TempDir(), "embedding-sidecar-test.sock")
	_ = os.Remove(udsPath)
	lis, err := net.Listen("unix", udsPath)
	if err != nil {
		t.Fatalf("listen uds: %v", err)
	}
	defer os.Remove(udsPath)

	grpcServer := grpc.NewServer()
	pb.RegisterEmbeddingServiceServer(grpcServer, handler)
	go grpcServer.Serve(lis)
	defer grpcServer.GracefulStop()

	conn, err := grpc.Dial(
		"unix://"+udsPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := pb.NewEmbeddingServiceClient(conn)
	resp, err := client.CheckLoop(ctx, &pb.CheckLoopRequest{
		TenantId: "tenant-grpc",
		Prompt:   "hello loop",
	})
	if err != nil {
		t.Fatalf("CheckLoop: %v", err)
	}
	if !resp.LoopDetected {
		t.Fatalf("expected loop_detected=true, got false (max_similarity=%v)", resp.MaxSimilarity)
	}
}


