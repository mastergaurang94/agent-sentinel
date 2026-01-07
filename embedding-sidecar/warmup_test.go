package main

import "testing"

type countingEmbedder struct {
	count int
	err   error
}

func (c *countingEmbedder) Compute(text string) ([]float32, error) {
	c.count++
	return []float32{0.1, 0.2, 0.3}, c.err
}

func TestWarmupEmbedder_Succeeds(t *testing.T) {
	emb := &countingEmbedder{}
	if err := warmupEmbedder(emb); err != nil {
		t.Fatalf("warmup failed: %v", err)
	}
	if emb.count != 1 {
		t.Fatalf("expected 1 call, got %d", emb.count)
	}
}

func TestWarmupEmbedder_Fails(t *testing.T) {
	emb := &countingEmbedder{err: errWarmupFail}
	if err := warmupEmbedder(emb); err == nil {
		t.Fatalf("expected error, got nil")
	}
}


